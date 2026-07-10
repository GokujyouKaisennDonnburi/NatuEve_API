package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ErrEventNotFound は参加対象のイベントが存在しない場合に返されるエラー。
var ErrEventNotFound = errors.New("event not found")

// ErrAlreadyJoined は同一イベントに重複して参加申込した場合に返されるエラー。
var ErrAlreadyJoined = errors.New("already joined")

// ErrEventCapacityFull は定員超過で参加できない場合に返されるエラー。
var ErrEventCapacityFull = errors.New("event capacity full")

// ErrEventCancelled は参加対象のイベントが取りやめになっている場合に返されるエラー。
var ErrEventCancelled = errors.New("event cancelled")

// pgUniqueViolationCode は PostgreSQL の unique_violation エラーコード。
const pgUniqueViolationCode = "23505"

// EventJoinRepository はイベント参加申込用Repositoryのインターフェース。
// Service層はこのInterfaceだけを知っていればよく、
// 実際のDB実装(PostgreSQLなど)には依存しない。
type EventJoinRepository interface {

	// Join はイベント参加を1トランザクションで登録する。成功時は member.CreatedAt を埋める。
	//
	// イベント行を FOR UPDATE でロックして存在確認・重複確認・定員確認・INSERT を
	// 原子的に行うため、並行リクエストでも定員超過・重複登録は発生しない。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrAlreadyJoined: 同一 mail_address（大文字小文字無視）またはログイン時は同一 profile_id で参加済み
	//   - ErrEventCapacityFull: 定員超過（定員 NULL / 0 は定員なし）
	Join(ctx context.Context, member *model.EventMember) error

	// ListRecipients は指定した eventID に参加登録済みの宛先一覧を返す。
	ListRecipients(ctx context.Context, eventID uuid.UUID) ([]model.EventRecipient, error)

	// ListMembers は指定 eventID の参加者一覧を作成日時の昇順で返す。
	// 0件の場合は nil ではなく空スライスを返す（呼び出し元の totalCount 計算で安全側に倒すため）。
	ListMembers(ctx context.Context, eventID uuid.UUID) ([]model.EventMember, error)
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

// Join はイベント参加を登録する。INSERT 後に RETURNING created_at で member.CreatedAt を埋める。
// member.ProfileID が Invalid（匿名参加）の場合は NULL として保存される。
func (r *eventJoinPostgres) Join(
	ctx context.Context,
	member *model.EventMember,
) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// イベント行をロックして存在確認・キャンセル状態確認・定員取得を同時に行う。
	// 同一イベントへの並行 join はこのロックで直列化される。
	const lockEvent = `
	SELECT capacity, cancelled_at
	FROM events
	WHERE id = $1
	FOR UPDATE
	`

	var (
		capacity    sql.NullInt32
		cancelledAt sql.NullTime
	)
	err = tx.QueryRowContext(ctx, lockEvent, member.EventID).Scan(&capacity, &cancelledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("event %s: %w", member.EventID, ErrEventNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock event: %w", err)
	}
	if cancelledAt.Valid {
		return fmt.Errorf("event %s: %w", member.EventID, ErrEventCancelled)
	}

	// 重複確認（同一 mail_address またはログイン時は同一 profile_id）。
	// profileID が Invalid（匿名参加）の場合、SQL 上 $3 は NULL になるため
	// `profile_id = NULL` は常に NULL（false 相当）となり mail_address のみで重複判定される。
	// mail_address は UNIQUE インデックス（lower(mail_address)）と同じ基準で比較する。
	const existsMember = `
	SELECT EXISTS(
		SELECT 1
		FROM event_members
		WHERE event_id = $1
		AND (
			lower(mail_address) = lower($2)
			OR profile_id = $3
		)
	)
	`

	var joined bool
	err = tx.QueryRowContext(
		ctx,
		existsMember,
		member.EventID,
		member.MailAddress,
		member.ProfileID,
	).Scan(&joined)
	if err != nil {
		return fmt.Errorf("exists member: %w", err)
	}
	if joined {
		return fmt.Errorf("event %s: %w", member.EventID, ErrAlreadyJoined)
	}

	// 定員確認。capacity が NULL または 0 は「定員なし」。
	// 人数は party_size の合計で数える（団体登録導入後もこの式のまま）。
	if capacity.Valid && capacity.Int32 > 0 {
		const sumPartySize = `
		SELECT COALESCE(SUM(party_size), 0)
		FROM event_members
		WHERE event_id = $1
		`

		var taken int
		if err := tx.QueryRowContext(ctx, sumPartySize, member.EventID).Scan(&taken); err != nil {
			return fmt.Errorf("sum party size: %w", err)
		}
		if taken+member.PartySize > int(capacity.Int32) {
			return fmt.Errorf("event %s: %w", member.EventID, ErrEventCapacityFull)
		}
	}

	const insertMember = `
	INSERT INTO event_members(
		id,
		event_id,
		profile_id,
		username,
		mail_address,
		party_size
	)
	VALUES(
		gen_random_uuid(),
		$1,
		$2,
		$3,
		$4,
		$5
	)
	RETURNING created_at
	`

	err = tx.QueryRowContext(
		ctx,
		insertMember,
		member.EventID,
		member.ProfileID,
		member.Username,
		member.MailAddress,
		member.PartySize,
	).Scan(&member.CreatedAt)
	if err != nil {
		// UNIQUE 制約違反は重複参加として扱う（事前チェックの最後の砦）。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
			return fmt.Errorf("event %s: %w", member.EventID, ErrAlreadyJoined)
		}
		return fmt.Errorf("join event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ListRecipients は指定した eventID に参加登録済みの宛先一覧を返す。
func (r *eventJoinPostgres) ListRecipients(ctx context.Context, eventID uuid.UUID) ([]model.EventRecipient, error) {
	// 参加登録順で返す（送信順を決定的にし、ログ・監査での追跡を容易にする）。
	const query = `
	SELECT mail_address
	FROM event_members
	WHERE event_id = $1
	ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("list recipients: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var recipients []model.EventRecipient
	for rows.Next() {
		var recipient model.EventRecipient
		if err := rows.Scan(&recipient.MailAddress); err != nil {
			return nil, fmt.Errorf("scan recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recipients rows: %w", err)
	}

	return recipients, nil
}

// ListMembers は指定 eventID の参加者一覧を作成日時の昇順で返す。
// profile_id は nullable なので uuid.NullUUID で受ける。
// レコードが 0 件でも nil ではなく空スライスを返す。
func (r *eventJoinPostgres) ListMembers(ctx context.Context, eventID uuid.UUID) ([]model.EventMember, error) {
	const query = `
	SELECT event_id, profile_id, username, mail_address, party_size, created_at
	FROM event_members
	WHERE event_id = $1
	ORDER BY created_at
	`

	rows, err := r.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// 0 件でも空スライスを返す（呼び出し元の totalCount 計算で安全側に倒すため）。
	members := []model.EventMember{}
	for rows.Next() {
		var m model.EventMember
		if err := rows.Scan(
			&m.EventID,
			&m.ProfileID,
			&m.Username,
			&m.MailAddress,
			&m.PartySize,
			&m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list members rows: %w", err)
	}

	return members, nil
}
