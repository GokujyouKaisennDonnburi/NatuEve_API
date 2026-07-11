package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// EventNotificationOutboxRepository は event_notification_outbox テーブルへのアクセスを
// 抽象化する。
//
// 単一プロセスのワーカー（internal/service.NotificationOutboxWorker）から呼ぶ前提の
// 実装になっている。複数インスタンス化してワーカーを並列実行する場合、ListDue が
// 返した行を複数ワーカーが同時に処理してしまう恐れがあるため、
// `SELECT ... FOR UPDATE SKIP LOCKED` 等による claim 処理を別途追加する必要がある
// （現状は未対応）。
type EventNotificationOutboxRepository interface {
	// ListDue は status='pending' かつ next_attempt_at <= now の行を
	// next_attempt_at 昇順で最大 limit 件取得する。
	ListDue(ctx context.Context, now time.Time, limit int) ([]model.EventNotificationOutbox, error)
	// MarkSent は指定行を送信済み（status='sent', sent_at=now）にする。
	MarkSent(ctx context.Context, id uuid.UUID) error
	// MarkRetry は送信失敗を記録し、attempts をインクリメントしたうえで
	// 次回試行日時(nextAttemptAt)・直近のエラー内容(lastError)を更新する。
	// status は pending のまま維持する。
	MarkRetry(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error
	// MarkFailed は最大試行回数に達した行を status='failed' にし、
	// attempts をインクリメントしたうえで直近のエラー内容を記録する。
	MarkFailed(ctx context.Context, id uuid.UUID, lastError string) error
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

// ListDue は送信対象（status='pending' かつ next_attempt_at <= now）の行を
// next_attempt_at 昇順で取得する。
func (r *eventNotificationOutboxPostgres) ListDue(ctx context.Context, now time.Time, limit int) ([]model.EventNotificationOutbox, error) {
	const query = `
		SELECT id, event_id, subject, body, status, attempts, next_attempt_at,
		       last_error, sent_at, created_at, updated_at
		FROM event_notification_outbox
		WHERE status = $1 AND next_attempt_at <= $2
		ORDER BY next_attempt_at ASC
		LIMIT $3`

	rows, err := r.db.QueryContext(ctx, query, model.NotificationOutboxStatusPending, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due notification outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []model.EventNotificationOutbox
	for rows.Next() {
		var (
			item      model.EventNotificationOutbox
			lastError sql.NullString
			sentAt    sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&item.EventID,
			&item.Subject,
			&item.Body,
			&item.Status,
			&item.Attempts,
			&item.NextAttemptAt,
			&lastError,
			&sentAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification outbox: %w", err)
		}
		item.LastError = lastError.String
		if sentAt.Valid {
			item.SentAt = &sentAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification outbox: %w", err)
	}

	return items, nil
}

// MarkSent は指定行を送信済みにする。
func (r *eventNotificationOutboxPostgres) MarkSent(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE event_notification_outbox
		SET status = $2, sent_at = now(), updated_at = now()
		WHERE id = $1`

	if _, err := r.db.ExecContext(ctx, query, id, model.NotificationOutboxStatusSent); err != nil {
		return fmt.Errorf("mark notification outbox sent: %w", err)
	}
	return nil
}

// MarkRetry は送信失敗を記録し、次回試行日時を更新する（status は pending のまま）。
func (r *eventNotificationOutboxPostgres) MarkRetry(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error {
	const query = `
		UPDATE event_notification_outbox
		SET attempts = attempts + 1,
		    next_attempt_at = $2,
		    last_error = $3,
		    updated_at = now()
		WHERE id = $1`

	if _, err := r.db.ExecContext(ctx, query, id, nextAttemptAt, lastError); err != nil {
		return fmt.Errorf("mark notification outbox retry: %w", err)
	}
	return nil
}

// MarkFailed は最大試行回数に達した行を failed にする。
func (r *eventNotificationOutboxPostgres) MarkFailed(ctx context.Context, id uuid.UUID, lastError string) error {
	const query = `
		UPDATE event_notification_outbox
		SET status = $2,
		    attempts = attempts + 1,
		    last_error = $3,
		    updated_at = now()
		WHERE id = $1`

	if _, err := r.db.ExecContext(ctx, query, id, model.NotificationOutboxStatusFailed, lastError); err != nil {
		return fmt.Errorf("mark notification outbox failed: %w", err)
	}
	return nil
}
