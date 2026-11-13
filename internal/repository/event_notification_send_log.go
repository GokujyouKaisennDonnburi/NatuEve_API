package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotificationSendLimitReached は一斉送信の回数上限に達し、枠を確保できなかった
// ことを表す。
//
// Reserve が返す。service 層は errors.Is で判定して呼び出し側に伝え、handler 層は
// HTTP 429 を返す。
var ErrNotificationSendLimitReached = errors.New("notification send limit reached")

// EventNotificationSendLogRepository は event_notification_send_log テーブルへの
// アクセスを抽象化する（ADR-0046）。
//
// 一斉送信 1 回につき予約行 1 行を用い、直近のウィンドウ内の行数で送信回数を数える。
// 行の追加は Reserve だけが行う。Reserve は同一イベントへの同時実行を
// pg_advisory_xact_lock で直列化するため、複数インスタンスから同時に呼ばれても
// 上限を超えて枠を確保することはない。
type EventNotificationSendLogRepository interface {
	// Reserve は eventID の一斉送信 1 回分の枠を確保する。
	//
	// now より window だけ過去までの期間に予約済みの行が limit 件以上あるときは
	// ErrNotificationSendLimitReached を返し、枠を確保しない。
	// 成功時は予約行の ID を返す。呼び出し側は送信成功後に MarkSent、送信失敗時に
	// Delete を必ず呼んで予約行を確定させること。
	Reserve(ctx context.Context, eventID uuid.UUID, now time.Time, window time.Duration, limit int) (int64, error)
	// MarkSent は予約行を送信済み（sent_at = now）にする。
	MarkSent(ctx context.Context, id int64, now time.Time) error
	// Delete は予約行を削除して枠を返す。送信失敗時など、送信が行われなかった予約の後始末に使う。
	Delete(ctx context.Context, id int64) error
}

// eventNotificationSendLogPostgres は EventNotificationSendLogRepository の
// PostgreSQL 実装。
type eventNotificationSendLogPostgres struct {
	db *sql.DB
}

// NewEventNotificationSendLogRepository は *sql.DB を使う
// EventNotificationSendLogRepository を生成する。
func NewEventNotificationSendLogRepository(db *sql.DB) EventNotificationSendLogRepository {
	return &eventNotificationSendLogPostgres{db: db}
}

// Reserve は送信回数上限の枠を確保する（ADR-0046）。
//
// トランザクション内で pg_advisory_xact_lock により同一イベントの予約を直列化してから
// 期間内の行数を数え、上限未満のときだけ INSERT する。数える（SELECT）と確保する
// （INSERT）の間に他の処理が割り込めないため、複数インスタンスから同時に呼ばれても
// limit 件を超えて枠を確保することはない。
func (r *eventNotificationSendLogPostgres) Reserve(
	ctx context.Context,
	eventID uuid.UUID,
	now time.Time,
	window time.Duration,
	limit int,
) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin reserve notification send: %w", err)
	}

	// 同一イベントへの予約を直列化する。キーはイベント UUID のハッシュ（bigint）。
	// 異なるイベントが同じハッシュに衝突しても、一時的に直列化されるだけで結果は変わらない。
	const lockQuery = `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`
	if _, err := tx.ExecContext(ctx, lockQuery, eventID.String()); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("lock notification send counter: %w", err)
	}

	// ウィンドウ内の予約行数を数える（境界の行は期間外として除外するため > で比較する）。
	cutoff := now.Add(-window)
	var count int
	const countQuery = `
		SELECT count(*)
		FROM event_notification_send_log
		WHERE event_id = $1 AND reserved_at > $2`
	if err := tx.QueryRowContext(ctx, countQuery, eventID, cutoff).Scan(&count); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("count notification send: %w", err)
	}
	if count >= limit {
		_ = tx.Rollback()
		return 0, ErrNotificationSendLimitReached
	}

	const insertQuery = `
		INSERT INTO event_notification_send_log (event_id, reserved_at)
		VALUES ($1, $2)
		RETURNING id`
	var id int64
	if err := tx.QueryRowContext(ctx, insertQuery, eventID, now).Scan(&id); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("insert notification send log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit reserve notification send: %w", err)
	}
	return id, nil
}

// MarkSent は予約行に送信日時を記録する。
func (r *eventNotificationSendLogPostgres) MarkSent(ctx context.Context, id int64, now time.Time) error {
	const query = `
		UPDATE event_notification_send_log
		SET sent_at = $2
		WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, query, id, now); err != nil {
		return fmt.Errorf("mark notification send log sent: %w", err)
	}
	return nil
}

// Delete は予約行を削除して枠を返す。
func (r *eventNotificationSendLogPostgres) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM event_notification_send_log WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("delete notification send log: %w", err)
	}
	return nil
}
