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
	// （Service 層で「未参加」として扱う）。
	// それ以外のエラーは fmt.Errorf でラップして返す。
	GetLatest(ctx context.Context, eventID, profileID uuid.UUID) (model.EventParticipationLog, error)
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
// ORDER BY created_at DESC LIMIT 1 で取得する。
func (r *eventParticipationLogPostgres) GetLatest(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.EventParticipationLog, error) {
	const query = `
	SELECT id, event_id, profile_id, action, created_at
	FROM event_participation_logs
	WHERE event_id = $1 AND profile_id = $2
	ORDER BY created_at DESC
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
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventParticipationLog{}, fmt.Errorf("latest participation log: %w", err)
	}
	if err != nil {
		return model.EventParticipationLog{}, fmt.Errorf("get latest participation log: %w", err)
	}

	return log, nil
}
