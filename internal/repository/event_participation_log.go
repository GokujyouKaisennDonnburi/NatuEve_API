package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// EventParticipationLogRepository は event_participation_logs テーブルへの
// アクセスを抽象化する。Service 層はこの Interface だけを知っていればよく、
// 実際の DB 実装(PostgreSQL など)には依存しない。
type EventParticipationLogRepository interface {
	// GetLatest は指定 eventID・profileID の最新の参加アクションを取得する。
	//
	// 履歴が1件も存在しない場合は sql.ErrNoRows を %w でラップして返す
	// （Service 層で「未参加」として扱う）。created_at が同値の行がある場合は
	// id DESC で決定的にタイブレークする。
	GetLatest(ctx context.Context, eventID, profileID uuid.UUID) (model.EventParticipationLog, error)

	// Create はイベント参加状態ログ(join/leave)を1件追記する。成功時は log.ID と log.CreatedAt を埋める。
	// 状態検証は行わず、指定された action をそのまま INSERT する（追記のみ方針）。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	Create(ctx context.Context, log *model.EventParticipationLog) error
}

// eventParticipationLogPostgres は PostgreSQL 実装。
type eventParticipationLogPostgres struct {
	db *sql.DB
}

// NewEventParticipationLogRepository は Repository を生成する。
func NewEventParticipationLogRepository(db *sql.DB) EventParticipationLogRepository {
	return &eventParticipationLogPostgres{
		db: db,
	}
}

// GetLatest は指定 eventID・profileID の最新の参加アクションを
// ORDER BY created_at DESC, id DESC LIMIT 1 で取得する。
// created_at が同値の複数行がある場合に id DESC で決定的なタイブレークを効かせる。
func (r *eventParticipationLogPostgres) GetLatest(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.EventParticipationLog, error) {
	const query = `
	SELECT id, event_id, profile_id, action, created_at
	FROM event_participation_logs
	WHERE event_id = $1 AND profile_id = $2
	ORDER BY created_at DESC, id DESC
	LIMIT 1
	`

	var log model.EventParticipationLog
	err := r.db.QueryRowContext(ctx, query, eventID, profileID).Scan(
		&log.ID,
		&log.EventID,
		&log.ProfileID,
		&log.Action,
		&log.CreatedAt,
	)
	if err != nil {
		return model.EventParticipationLog{}, fmt.Errorf("get latest participation log: %w", err)
	}

	return log, nil
}

// Create はイベント参加状態ログを追記する。INSERT 後に RETURNING id, created_at で
// log.ID と log.CreatedAt を埋める。トランザクションやロックは不要（追記のみ・状態検証なし）。
func (r *eventParticipationLogPostgres) Create(
	ctx context.Context,
	log *model.EventParticipationLog,
) error {

	// イベント存在確認。
	const checkEvent = `
	SELECT 1
	FROM events
	WHERE id = $1
	`

	var exists int
	err := r.db.QueryRowContext(ctx, checkEvent, log.EventID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("event %s: %w", log.EventID, ErrEventNotFound)
	}
	if err != nil {
		return fmt.Errorf("check event: %w", err)
	}

	const insertLog = `
	INSERT INTO event_participation_logs(
		event_id,
		profile_id,
		action
	)
	VALUES($1, $2, $3)
	RETURNING id, created_at
	`

	err = r.db.QueryRowContext(
		ctx,
		insertLog,
		log.EventID,
		log.ProfileID,
		log.Action,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("create participation log: %w", err)
	}

	return nil
}
