package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// EventNotificationOutboxRepository は event_notification_outbox テーブルへのアクセスを
// 抽象化する。
//
// 行の取得は ClaimNextDue が `FOR UPDATE SKIP LOCKED` で行う。複数の処理が同時に走っても、
// 同じ行を同時に確保することはない（ADR-0034）。
type EventNotificationOutboxRepository interface {
	// ClaimNextDue は status='pending' かつ next_attempt_at <= now の行のうち、
	// next_attempt_at が最も古い 1 件を確保して返す。他の処理が確保中の行は飛ばす。
	// excludeIDs に含まれる行は対象外にする（同じ呼び出し内で処理済みの行を取り直さないため）。
	// 対象の行が無いときは (nil, nil) を返す。
	//
	// 返した OutboxClaim は、Mark* のいずれか、または Release を必ず呼んで終わらせること。
	ClaimNextDue(ctx context.Context, now time.Time, excludeIDs []uuid.UUID) (OutboxClaim, error)
}

// OutboxClaim は確保した outbox 行 1 件。確保している間、その行は他の処理から見えない。
// 内部はトランザクションで、Mark* は更新して確定（コミット）し、Release は何も変えずに手放す。
type OutboxClaim interface {
	// Item は確保した行を返す。
	Item() model.EventNotificationOutbox
	// Deadline は確保の期限を返す。これを過ぎるとトランザクションは巻き戻され、行は他の処理から確保できるようになる。
	// Mark* はこの期限より前に呼ぶこと。
	Deadline() time.Time
	// MarkSent は行を送信済み（status='sent', sent_at=now）にして確定する。
	MarkSent(ctx context.Context) error
	// MarkRetry は送信失敗を記録し、attempts をインクリメントしたうえで
	// 次回試行日時(nextAttemptAt)・直近のエラー内容(lastError)を更新して確定する。
	// status は pending のまま維持する。
	MarkRetry(ctx context.Context, nextAttemptAt time.Time, lastError string) error
	// MarkFailed は最大試行回数に達した行を status='failed' にし、
	// attempts をインクリメントしたうえで直近のエラー内容を記録して確定する。
	MarkFailed(ctx context.Context, lastError string) error
	// Release は行を変更せずに手放す。Mark* で確定した後に呼んでも何も起きない。
	Release() error
}

// eventNotificationOutboxPostgres は EventNotificationOutboxRepository の PostgreSQL 実装。
type eventNotificationOutboxPostgres struct {
	db *sql.DB
}

// NewEventNotificationOutboxRepository は *sql.DB を使う
// EventNotificationOutboxRepository を生成する。
func NewEventNotificationOutboxRepository(db *sql.DB) EventNotificationOutboxRepository {
	return &eventNotificationOutboxPostgres{db: db}
}

// outboxClaimTimeout は確保した行のトランザクションの寿命の上限。超えると巻き戻され、行は pending に戻る。
const outboxClaimTimeout = 2 * time.Minute

// ClaimNextDue は送信対象の行を 1 件確保する。
//
// 接続の取得と取得クエリは ctx に従う。トランザクションは context.WithoutCancel をもとに
// outboxClaimTimeout の期限付きで開始するため、ctx のキャンセルでは巻き戻されない（ADR-0034）。
func (r *eventNotificationOutboxPostgres) ClaimNextDue(ctx context.Context, now time.Time, excludeIDs []uuid.UUID) (OutboxClaim, error) {
	const query = `
		SELECT id, event_id, recipient_kind, subject, body, status, attempts, next_attempt_at,
		       last_error, sent_at, created_at, updated_at
		FROM event_notification_outbox
		WHERE status = $1 AND next_attempt_at <= $2
		  AND NOT (id::text = ANY(coalesce($3::text[], '{}')))
		ORDER BY next_attempt_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn for notification outbox: %w", err)
	}
	deadline := time.Now().Add(outboxClaimTimeout)
	txCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	tx, err := conn.BeginTx(txCtx, nil)
	if err != nil {
		cancel()
		_ = conn.Close()
		return nil, fmt.Errorf("begin claim notification outbox: %w", err)
	}
	claim := &outboxClaimPostgres{conn: conn, tx: tx, cancel: cancel, deadline: deadline}

	exclude := make([]string, 0, len(excludeIDs))
	for _, id := range excludeIDs {
		exclude = append(exclude, id.String())
	}

	var (
		item      model.EventNotificationOutbox
		lastError sql.NullString
		sentAt    sql.NullTime
	)
	err = tx.QueryRowContext(ctx, query, model.NotificationOutboxStatusPending, now, exclude).Scan(
		&item.ID,
		&item.EventID,
		&item.RecipientKind,
		&item.Subject,
		&item.Body,
		&item.Status,
		&item.Attempts,
		&item.NextAttemptAt,
		&lastError,
		&sentAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		_ = claim.Release()
		return nil, nil
	}
	if err != nil {
		_ = claim.Release()
		return nil, fmt.Errorf("claim notification outbox: %w", err)
	}
	item.LastError = lastError.String
	if sentAt.Valid {
		item.SentAt = &sentAt.Time
	}
	claim.item = item

	return claim, nil
}

// outboxClaimPostgres は OutboxClaim の PostgreSQL 実装。
// トランザクションの終了（コミット・巻き戻し）後に、期限の解除と接続の返却を行う。
type outboxClaimPostgres struct {
	conn     *sql.Conn
	tx       *sql.Tx
	cancel   context.CancelFunc
	deadline time.Time
	item     model.EventNotificationOutbox
}

// Item は確保した行を返す。
func (c *outboxClaimPostgres) Item() model.EventNotificationOutbox {
	return c.item
}

// Deadline は確保の期限を返す。
func (c *outboxClaimPostgres) Deadline() time.Time {
	return c.deadline
}

// MarkSent は確保した行を送信済みにして確定する。
func (c *outboxClaimPostgres) MarkSent(ctx context.Context) error {
	const query = `
		UPDATE event_notification_outbox
		SET status = $2, sent_at = now(), updated_at = now()
		WHERE id = $1`

	if err := c.exec(ctx, query, c.item.ID, model.NotificationOutboxStatusSent); err != nil {
		return fmt.Errorf("mark notification outbox sent: %w", err)
	}
	return nil
}

// MarkRetry は送信失敗を記録し、次回試行日時を更新して確定する（status は pending のまま）。
func (c *outboxClaimPostgres) MarkRetry(ctx context.Context, nextAttemptAt time.Time, lastError string) error {
	const query = `
		UPDATE event_notification_outbox
		SET attempts = attempts + 1,
		    next_attempt_at = $2,
		    last_error = $3,
		    updated_at = now()
		WHERE id = $1`

	if err := c.exec(ctx, query, c.item.ID, nextAttemptAt, lastError); err != nil {
		return fmt.Errorf("mark notification outbox retry: %w", err)
	}
	return nil
}

// MarkFailed は最大試行回数に達した行を failed にして確定する。
func (c *outboxClaimPostgres) MarkFailed(ctx context.Context, lastError string) error {
	const query = `
		UPDATE event_notification_outbox
		SET status = $2,
		    attempts = attempts + 1,
		    last_error = $3,
		    updated_at = now()
		WHERE id = $1`

	if err := c.exec(ctx, query, c.item.ID, model.NotificationOutboxStatusFailed, lastError); err != nil {
		return fmt.Errorf("mark notification outbox failed: %w", err)
	}
	return nil
}

// Release は確保した行を変更せずに手放す。確定済みなら何もしない。
func (c *outboxClaimPostgres) Release() error {
	err := c.tx.Rollback()
	c.finish()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("release notification outbox: %w", err)
	}
	return nil
}

// exec は更新を実行してコミットする。更新に失敗した場合は巻き戻す。
func (c *outboxClaimPostgres) exec(ctx context.Context, query string, args ...any) error {
	defer c.finish()
	if _, err := c.tx.ExecContext(ctx, query, args...); err != nil {
		_ = c.tx.Rollback()
		return err
	}
	return c.tx.Commit()
}

// finish はトランザクションの期限を解除し、接続をプールへ返す。複数回呼んでもよい。
func (c *outboxClaimPostgres) finish() {
	c.cancel()
	_ = c.conn.Close()
}
