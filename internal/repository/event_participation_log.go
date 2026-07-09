package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// EventParticipationLogRepository はイベント参加状態ログ追加用Repositoryのインターフェース。
// Service層はこのInterfaceだけを知っていればよく、
// 実際のDB実装(PostgreSQLなど)には依存しない。
type EventParticipationLogRepository interface {

	// Create はイベント参加状態ログ(join/leave)を1件追記する。成功時は log.ID と log.CreatedAt を埋める。
	// 状態検証は行わず、指定された action をそのまま INSERT する（追記のみ方針）。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	Create(ctx context.Context, log *model.EventParticipationLog) error
}

// eventParticipationLogPostgres は PostgreSQL実装。
type eventParticipationLogPostgres struct {
	db *sql.DB
}

// NewEventParticipationLogRepository はRepositoryを生成する。
func NewEventParticipationLogRepository(db *sql.DB) EventParticipationLogRepository {
	return &eventParticipationLogPostgres{
		db: db,
	}
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
