package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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

// ErrNotJoined は参加キャンセル時に、そのイベントに参加していない場合に返されるエラー。
var ErrNotJoined = errors.New("not joined")

// ErrCategoryNotFound は申込で指定されたカテゴリがそのイベントの費用カテゴリに存在しない場合に返されるエラー。
var ErrCategoryNotFound = errors.New("participant category not found")

// ErrDuplicateCategory は申込の内訳で同一カテゴリが複数指定された場合に返されるエラー。
// 大文字小文字違いなど、表記が異なっていても同じ費用カテゴリに解決された場合を含む。
var ErrDuplicateCategory = errors.New("duplicate participant category")

// pgUniqueViolationCode は PostgreSQL の unique_violation エラーコード。
const pgUniqueViolationCode = "23505"

// EventJoinRepository はイベント参加申込用Repositoryのインターフェース。
// Service層はこのInterfaceだけを知っていればよく、
// 実際のDB実装(PostgreSQLなど)には依存しない。
type EventJoinRepository interface {

	// Join はイベント参加を1トランザクションで登録する。
	// 成功時は member.ID・member.CreatedAt と、member.Categories の CostID・Category を埋める。
	//
	// イベント行を FOR UPDATE でロックして存在確認・重複確認・カテゴリ解決・定員確認・INSERT を
	// 原子的に行うため、並行リクエストでも定員超過・重複登録は発生しない。
	// member.Categories の Category（カテゴリ名）は event_costs から lower() 比較で
	// cost_id に解決し、event_member_categories へ内訳を追記する。
	// ログイン参加（member.ProfileID が Valid）の場合は、同一トランザクション内で
	// event_participation_logs に action='join' を1件、その内訳を
	// event_participation_log_categories に追記する。匿名参加（profile_id NULL）は
	// ログ対象外（event_participation_logs.profile_id が NOT NULL のため）。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrAlreadyJoined: 同一 mail_address（大文字小文字無視）またはログイン時は同一 profile_id で参加済み
	//   - ErrEventCapacityFull: 定員超過（定員 NULL / 0 は定員なし）
	//   - ErrCategoryNotFound: 指定カテゴリがそのイベントの費用カテゴリに存在しない
	//   - ErrDuplicateCategory: 同一カテゴリが内訳で重複している
	Join(ctx context.Context, member *model.EventMember) error

	// Leave はログイン参加者のイベント参加を1トランザクションで取り消す。
	//
	// event_members から (event_id, profile_id) 一致行を DELETE し、同一トランザクション内で
	// event_participation_logs に action='leave' を1件追記する。参加取消とログ追記を原子的に行い、
	// 片方だけ成功する不整合を防ぐ。成功時は追記した leave ログの created_at を返す。
	// 匿名参加（profile_id NULL）は profile_id で識別できず、本メソッドの対象外。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrNotJoined: そのイベントに参加していない（削除対象行なし）
	Leave(ctx context.Context, eventID, profileID uuid.UUID) (time.Time, error)

	// ListRecipients は指定した eventID に参加登録済みの宛先一覧を返す。
	ListRecipients(ctx context.Context, eventID uuid.UUID) ([]model.EventRecipient, error)

	// ListMembers は指定 eventID の参加者一覧を作成日時の昇順で返す。
	// profiles を LEFT JOIN してプロフィールサマリーを同時取得する。
	// 匿名参加（profile_id NULL）は EventMember.Profile が nil になる。
	// 0件の場合は nil ではなく空スライスを返す（呼び出し元の totalCount 計算で安全側に倒すため）。
	ListMembers(ctx context.Context, eventID uuid.UUID) ([]model.EventMember, error)

	// GetMemberByProfile は指定 eventID・profileID のログイン参加者の申込1件を返す。
	// Categories はカテゴリ名の昇順で返す。0件でも nil ではなく空スライスを返す。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrNotJoined: そのイベントに参加していない（未申込・キャンセル済み・匿名申込を含む）
	GetMemberByProfile(ctx context.Context, eventID, profileID uuid.UUID) (model.EventMember, error)
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

	// 申込のカテゴリ名を、そのイベントの費用カテゴリ（event_costs）へ解決する。
	// 照合は event_costs の一意インデックスと同じ lower(category) 基準で行うため、
	// 「一意と判定される表記」と「解決に成功する表記」が食い違わない。
	const findCost = `
	SELECT id, category
	FROM event_costs
	WHERE event_id = $1
	AND lower(category) = lower($2)
	`

	seenCost := make(map[uuid.UUID]struct{}, len(member.Categories))
	for i := range member.Categories {
		var (
			costID   uuid.UUID
			category string
		)
		err := tx.QueryRowContext(
			ctx,
			findCost,
			member.EventID,
			member.Categories[i].Category,
		).Scan(&costID, &category)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("category %q: %w", member.Categories[i].Category, ErrCategoryNotFound)
		}
		if err != nil {
			return fmt.Errorf("find cost: %w", err)
		}
		// 表記違いで同一カテゴリを指した場合もここで弾く（DB では PK 違反になる）。
		if _, dup := seenCost[costID]; dup {
			return fmt.Errorf("category %q: %w", member.Categories[i].Category, ErrDuplicateCategory)
		}
		seenCost[costID] = struct{}{}

		member.Categories[i].CostID = costID
		// 保存・返却するカテゴリ名は event_costs 側の表記に揃える。
		member.Categories[i].Category = category
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
	RETURNING id, created_at
	`

	err = tx.QueryRowContext(
		ctx,
		insertMember,
		member.EventID,
		member.ProfileID,
		member.Username,
		member.MailAddress,
		member.PartySize,
	).Scan(&member.ID, &member.CreatedAt)
	if err != nil {
		// UNIQUE 制約違反は重複参加として扱う（事前チェックの最後の砦）。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
			return fmt.Errorf("event %s: %w", member.EventID, ErrAlreadyJoined)
		}
		return fmt.Errorf("join event: %w", err)
	}

	// カテゴリ別人数の内訳を追記する。
	const insertMemberCategory = `
	INSERT INTO event_member_categories(
		member_id,
		cost_id,
		event_id,
		head_count
	)
	VALUES($1, $2, $3, $4)
	`

	for _, c := range member.Categories {
		if _, err := tx.ExecContext(
			ctx,
			insertMemberCategory,
			member.ID,
			c.CostID,
			member.EventID,
			c.HeadCount,
		); err != nil {
			return fmt.Errorf("insert member category: %w", err)
		}
	}

	// ログイン参加時のみ、参加状態ログに join を追記する（同一トランザクション内で原子的に）。
	// event_participation_logs.profile_id は NOT NULL のため、匿名参加（profile_id NULL）は
	// ログ記録の対象外とする。参加登録とログ追記を1トランザクションにまとめることで、
	// 片方だけ成功する不整合を防ぐ。
	if member.ProfileID.Valid {
		const insertParticipationLog = `
		INSERT INTO event_participation_logs(
			event_id,
			profile_id,
			action
		)
		VALUES($1, $2, 'join')
		RETURNING id
		`

		var logID uuid.UUID
		if err := tx.QueryRowContext(
			ctx,
			insertParticipationLog,
			member.EventID,
			member.ProfileID.UUID,
		).Scan(&logID); err != nil {
			return fmt.Errorf("insert participation log: %w", err)
		}

		// join ログにも同じ内訳を残す。
		const insertLogCategory = `
		INSERT INTO event_participation_log_categories(
			participation_log_id,
			cost_id,
			event_id,
			head_count
		)
		VALUES($1, $2, $3, $4)
		`

		for _, c := range member.Categories {
			if _, err := tx.ExecContext(
				ctx,
				insertLogCategory,
				logID,
				c.CostID,
				member.EventID,
				c.HeadCount,
			); err != nil {
				return fmt.Errorf("insert participation log category: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Leave はログイン参加者のイベント参加を取り消す。
// event_members から (event_id, profile_id) 一致行を DELETE し、同一トランザクション内で
// event_participation_logs に action='leave' を1件追記して、その created_at を返す。
func (r *eventJoinPostgres) Leave(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (time.Time, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 参加行を削除する。ログイン参加者は1イベントにつき高々1行のため、
	// (event_id, profile_id) で一意に対象を特定できる。
	const deleteMember = `
	DELETE FROM event_members
	WHERE event_id = $1
	AND profile_id = $2
	`

	res, err := tx.ExecContext(ctx, deleteMember, eventID, profileID)
	if err != nil {
		return time.Time{}, fmt.Errorf("delete member: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return time.Time{}, fmt.Errorf("rows affected: %w", err)
	}

	// 削除対象が無い場合、イベント不存在と未参加を区別してエラーを返す。
	if affected == 0 {
		const existsEvent = `SELECT EXISTS(SELECT 1 FROM events WHERE id = $1)`
		var exists bool
		if err := tx.QueryRowContext(ctx, existsEvent, eventID).Scan(&exists); err != nil {
			return time.Time{}, fmt.Errorf("exists event: %w", err)
		}
		if !exists {
			return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrNotJoined)
	}

	// 参加取消と同時に、参加状態ログへ leave を追記する（同一トランザクション内で原子的に）。
	// leave は認証必須のため profile_id は常に非 NULL で、NOT NULL 制約を満たす。
	const insertParticipationLog = `
	INSERT INTO event_participation_logs(
		event_id,
		profile_id,
		action
	)
	VALUES($1, $2, 'leave')
	RETURNING created_at
	`

	var createdAt time.Time
	if err := tx.QueryRowContext(
		ctx,
		insertParticipationLog,
		eventID,
		profileID,
	).Scan(&createdAt); err != nil {
		return time.Time{}, fmt.Errorf("insert participation log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, fmt.Errorf("commit transaction: %w", err)
	}

	return createdAt, nil
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
// profiles を LEFT JOIN してプロフィールサマリー（display_name・avatar_url）を同時取得する。
//
// 匿名参加（profile_id NULL）は Profile を nil にする。event.go の ListSummaries が
// NULL を空文字で埋めた model.ProfileSummary として返すのとは意図的に異なる扱いで、
// 「プロフィールが存在しない（匿名）」と「アイコン未設定（空文字）」を呼び出し元が
// 区別できるようにするため。
//
// profiles.id は PK、event_members には (event_id, created_at) の複合インデックス
// （migration 20260708052650）があるため、JOIN を足しても WHERE / ORDER BY のコストは変わらない。
//
// レコードが 0 件でも nil ではなく空スライスを返す。
func (r *eventJoinPostgres) ListMembers(ctx context.Context, eventID uuid.UUID) ([]model.EventMember, error) {
	const query = `
	SELECT m.id, m.event_id, m.profile_id, m.username, m.mail_address, m.party_size, m.created_at,
	       p.id, p.display_name, p.avatar_url
	FROM event_members m
	LEFT JOIN profiles p ON p.id = m.profile_id
	WHERE m.event_id = $1
	ORDER BY m.created_at
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
		var (
			pID         sql.NullString
			displayName sql.NullString
			avatarURL   sql.NullString
		)
		if err := rows.Scan(
			&m.ID,
			&m.EventID,
			&m.ProfileID,
			&m.Username,
			&m.MailAddress,
			&m.PartySize,
			&m.CreatedAt,
			&pID,
			&displayName,
			&avatarURL,
		); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		if pID.Valid {
			m.Profile = &model.ProfileSummary{
				ID:          pID.String,
				DisplayName: displayName.String,
				AvatarURL:   avatarURL.String,
			}
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list members rows: %w", err)
	}

	// 内訳は1クエリでまとめて引き、member_id で紐づける（申込ごとに引くと N+1 になる）。
	categories, err := r.listMemberCategories(ctx, eventID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		members[i].Categories = categories[members[i].ID]
	}

	return members, nil
}

// GetMemberByProfile は指定 eventID・profileID のログイン参加者の申込1件を返す。
// 参照のみで更新を伴わないため（申込後に内訳が変わる更新経路が無いため）、
// トランザクションは使わない。
func (r *eventJoinPostgres) GetMemberByProfile(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.EventMember, error) {
	const query = `
	SELECT id, username, mail_address, party_size, created_at
	FROM event_members
	WHERE event_id = $1
	AND profile_id = $2
	`

	member := model.EventMember{
		EventID:   eventID,
		ProfileID: uuid.NullUUID{UUID: profileID, Valid: true},
	}
	err := r.db.QueryRowContext(ctx, query, eventID, profileID).Scan(
		&member.ID,
		&member.Username,
		&member.MailAddress,
		&member.PartySize,
		&member.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		const existsEvent = `SELECT EXISTS(SELECT 1 FROM events WHERE id = $1)`
		var exists bool
		if err := r.db.QueryRowContext(ctx, existsEvent, eventID).Scan(&exists); err != nil {
			return model.EventMember{}, fmt.Errorf("exists event: %w", err)
		}
		if !exists {
			return model.EventMember{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		return model.EventMember{}, fmt.Errorf("event %s: %w", eventID, ErrNotJoined)
	}
	if err != nil {
		return model.EventMember{}, fmt.Errorf("get member by profile: %w", err)
	}

	const categoriesQuery = `
	SELECT mc.cost_id, c.category, mc.head_count
	FROM event_member_categories mc
	JOIN event_costs c ON c.id = mc.cost_id
	WHERE mc.member_id = $1
	ORDER BY c.category
	`

	rows, err := r.db.QueryContext(ctx, categoriesQuery, member.ID)
	if err != nil {
		return model.EventMember{}, fmt.Errorf("list member categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// 0件でも空スライスを返す（呼び出し元の Participants を null ではなく [] にするため）。
	categories := []model.MemberCategory{}
	for rows.Next() {
		var c model.MemberCategory
		if err := rows.Scan(&c.CostID, &c.Category, &c.HeadCount); err != nil {
			return model.EventMember{}, fmt.Errorf("scan member category: %w", err)
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return model.EventMember{}, fmt.Errorf("list member categories rows: %w", err)
	}
	member.Categories = categories

	return member, nil
}

// listMemberCategories は指定 eventID の参加者内訳を member_id ごとにまとめて返す。
// カテゴリ名の昇順で並べる（event_costs に表示順の列がないため、順序を決定的にする）。
// 内訳を持たない申込はキー自体が存在しない（呼び出し元では nil スライスになる）。
func (r *eventJoinPostgres) listMemberCategories(
	ctx context.Context,
	eventID uuid.UUID,
) (map[uuid.UUID][]model.MemberCategory, error) {
	const query = `
	SELECT mc.member_id, mc.cost_id, c.category, mc.head_count
	FROM event_member_categories mc
	JOIN event_costs c ON c.id = mc.cost_id
	WHERE mc.event_id = $1
	ORDER BY c.category
	`

	rows, err := r.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("list member categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byMember := map[uuid.UUID][]model.MemberCategory{}
	for rows.Next() {
		var (
			memberID uuid.UUID
			c        model.MemberCategory
		)
		if err := rows.Scan(&memberID, &c.CostID, &c.Category, &c.HeadCount); err != nil {
			return nil, fmt.Errorf("scan member category: %w", err)
		}
		byMember[memberID] = append(byMember[memberID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list member categories rows: %w", err)
	}

	return byMember, nil
}
