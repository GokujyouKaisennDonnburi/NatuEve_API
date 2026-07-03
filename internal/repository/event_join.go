package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// EventJoinRepository はイベント参加申込用Repositoryのインターフェース。
// Service層はこのInterfaceだけを知っていればよく、
// 実際のDB実装(PostgreSQLなど)には依存しない。
type EventJoinRepository interface {

	// ExistsEvent はイベントが存在するか確認する。
	ExistsEvent(ctx context.Context, eventID uuid.UUID) (bool, error)

	// ExistsMember は同一イベントに同じ mail_address、またはログイン時は同じ profile_id の
	// 参加レコードが存在するか確認する。profileID が Invalid（匿名）の場合は mail_address のみで判定する。
	ExistsMember(ctx context.Context, eventID uuid.UUID, profileID uuid.NullUUID, mailAddress string) (bool, error)

	// CountMembers は現在の参加人数を取得する。
	CountMembers(ctx context.Context, eventID uuid.UUID) (int, error)

	// GetCapacity はイベントの定員を取得する。NULL（定員なし）の場合は 0 を返す。
	GetCapacity(ctx context.Context, eventID uuid.UUID) (int, error)

	// Join はイベント参加を登録する。成功時は member.CreatedAt を埋める。
	Join(ctx context.Context, member *model.EventMember) error
}

// eventJoinPostgres は PostgreSQL実装。
type eventJoinPostgres struct {
	db *sql.DB
}

// NewEventJoinRepository はRepositoryを生成する。
func NewEventJoinRepository(db *sql.DB) EventJoinRepository {
	return &eventJoinPostgres{
		db: db,
	}
}

// ExistsEvent はイベントが存在するか確認する。
func (r *eventJoinPostgres) ExistsEvent(
	ctx context.Context,
	eventID uuid.UUID,
) (bool, error) {

	const query = `
	SELECT EXISTS(
		SELECT 1
		FROM events
		WHERE id = $1
	)
	`

	var exists bool

	err := r.db.QueryRowContext(
		ctx,
		query,
		eventID,
	).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("exists event: %w", err)
	}

	return exists, nil
}

// ExistsMember は同一イベントに同じ mail_address、またはログイン時は同じ profile_id の
// 参加レコードが存在するか確認する。
//
// profileID が Invalid（匿名参加）の場合、SQL 上 $3 は NULL になるため
// `profile_id = NULL` は常に NULL（false 相当）となり mail_address のみで重複判定される。
func (r *eventJoinPostgres) ExistsMember(
	ctx context.Context,
	eventID uuid.UUID,
	profileID uuid.NullUUID,
	mailAddress string,
) (bool, error) {

	const query = `
	SELECT EXISTS(
		SELECT 1
		FROM event_members
		WHERE event_id = $1
		AND (
			mail_address = $2
			OR profile_id = $3
		)
	)
	`

	var exists bool

	err := r.db.QueryRowContext(
		ctx,
		query,
		eventID,
		mailAddress,
		profileID,
	).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("exists member: %w", err)
	}

	return exists, nil
}

// CountMembers は現在の参加人数を取得する。
func (r *eventJoinPostgres) CountMembers(
	ctx context.Context,
	eventID uuid.UUID,
) (int, error) {

	const query = `
	SELECT COUNT(*)
	FROM event_members
	WHERE event_id = $1
	`

	var count int

	err := r.db.QueryRowContext(
		ctx,
		query,
		eventID,
	).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}

	return count, nil
}

// GetCapacity はイベントの定員を取得する。capacity が NULL（定員なし）の場合は 0 を返す。
func (r *eventJoinPostgres) GetCapacity(
	ctx context.Context,
	eventID uuid.UUID,
) (int, error) {

	const query = `
	SELECT capacity
	FROM events
	WHERE id = $1
	`

	var capacity sql.NullInt32

	err := r.db.QueryRowContext(
		ctx,
		query,
		eventID,
	).Scan(&capacity)

	if err != nil {
		return 0, fmt.Errorf("get capacity: %w", err)
	}

	// NULL は「定員なし」を表す。0 を返すことで service 層が定員なしと判定できる。
	if !capacity.Valid {
		return 0, nil
	}

	return int(capacity.Int32), nil
}

// Join はイベント参加を登録する。INSERT 後に RETURNING created_at で member.CreatedAt を埋める。
// member.ProfileID が Invalid（匿名参加）の場合は NULL として保存される。
func (r *eventJoinPostgres) Join(
	ctx context.Context,
	member *model.EventMember,
) error {

	const query = `
	INSERT INTO event_members(
		id,
		event_id,
		profile_id,
		username,
		mail_address
	)
	VALUES(
		gen_random_uuid(),
		$1,
		$2,
		$3,
		$4
	)
	RETURNING created_at
	`

	err := r.db.QueryRowContext(
		ctx,
		query,
		member.EventID,
		member.ProfileID,
		member.Username,
		member.MailAddress,
	).Scan(&member.CreatedAt)

	if err != nil {
		return fmt.Errorf("join event: %w", err)
	}

	return nil
}
