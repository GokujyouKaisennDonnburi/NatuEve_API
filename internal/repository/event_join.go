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

// ErrDeadlinePassed は参加申込・参加キャンセル時に、申込期限（events.application_deadline）が
// 経過している場合に返されるエラー（ADR-0029・ADR-0031）。
var ErrDeadlinePassed = errors.New("deadline passed")

// ErrAbsenceBeforeDeadline は欠席連絡時に、申込期限（events.application_deadline）前
// である場合に返されるエラー（ADR-0031）。
var ErrAbsenceBeforeDeadline = errors.New("absence before deadline")

// ErrEventEnded は参加申込・欠席連絡時に、イベントの end_date が経過している場合に
// 返されるエラー（ADR-0029・ADR-0031）。
var ErrEventEnded = errors.New("event ended")

// ErrCategoryNotFound は申込で指定されたカテゴリがそのイベントの費用カテゴリに存在しない場合に返されるエラー。
var ErrCategoryNotFound = errors.New("participant category not found")

// ErrDuplicateCategory は申込の内訳で同一カテゴリが複数指定された場合に返されるエラー。
// 大文字小文字違いなど、表記が異なっていても同じ費用カテゴリに解決された場合を含む。
var ErrDuplicateCategory = errors.New("duplicate participant category")

// ErrConfirmationTokenInvalid は確認トークンが不正・期限切れ・使用済みの場合に返されるエラー（ADR-0044）。
var ErrConfirmationTokenInvalid = errors.New("confirmation token invalid")

// JoinOutcome は Join の結果を表す（ADR-0044）。
type JoinOutcome string

const (
	// JoinOutcomePending は未確定（pending）で登録したことを表す。
	JoinOutcomePending JoinOutcome = "pending"
	// JoinOutcomeConfirmed は確定（confirmed）で登録したことを表す。
	JoinOutcomeConfirmed JoinOutcome = "confirmed"
	// JoinOutcomeAlreadyJoined は同一メールアドレスの申込が既にあり、
	// 新規申込と同じ応答を返すことを表す。
	JoinOutcomeAlreadyJoined JoinOutcome = "already_joined"
	// JoinOutcomeAlreadyJoinedSelf はログイン中の本人が既に参加していることを表す。
	JoinOutcomeAlreadyJoinedSelf JoinOutcome = "already_joined_self"
	// JoinOutcomeCapacityFull は定員超過を表す。
	JoinOutcomeCapacityFull JoinOutcome = "capacity_full"
)

// pgUniqueViolationCode は PostgreSQL の unique_violation エラーコード。
const pgUniqueViolationCode = "23505"

// EventJoinRepository はイベント参加申込用Repositoryのインターフェース。
// Service層はこのInterfaceだけを知っていればよく、
// 実際のDB実装(PostgreSQLなど)には依存しない。
type EventJoinRepository interface {

	// Join はイベント参加を1トランザクションで登録する（ADR-0044）。
	// 成功時は member.ID・member.CreatedAt と、member.Categories の CostID・Category を埋める。
	//
	// イベント行を FOR UPDATE でロックして存在確認・重複確認・カテゴリ解決・定員確認・INSERT を
	// 原子的に行うため、並行リクエストでも定員超過・重複登録は発生しない。
	// member.Categories の Category（カテゴリ名）は event_costs から lower() 比較で
	// cost_id に解決し、event_member_categories へ内訳を追記する。
	//
	// member.Status は pending / confirmed のいずれかを指定する。pending の場合は
	// ConfirmationTokenHash・ConfirmationTokenExpiresAt を保存し、notification の
	// Confirmation* を recipient_kind='email' の outbox に予約する。confirmed の場合は
	// トークン列を NULL にし、ログイン参加（ProfileID が Valid）なら同一トランザクション内で
	// event_participation_logs に action='join' を1件、その内訳を
	// event_participation_log_categories に追記する（匿名参加はログ対象外）。
	//
	// 同一メールアドレスの申込が既にある場合は、notification の Notice* を outbox に予約し、
	// JoinOutcomeAlreadyJoined を返す（新規申込と同じ応答にするため、エラーにはしない）。
	// ログイン中の本人（同一 profile_id）が既に参加している場合は JoinOutcomeAlreadyJoinedSelf、
	// 定員超過は JoinOutcomeCapacityFull を返す。定員判定は confirmed だけを数え、
	// 重複申込でも新規申込と同じ条件で判定する。
	// ログイン参加で同一メールの pending 行がある場合は、その行を削除して confirmed で登録する。
	//
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrEventCancelled: イベントが取りやめになっている
	//   - ErrDeadlinePassed: 申込期限ありイベントで期限経過後（ADR-0029）
	//   - ErrEventEnded: end_date 経過後（ADR-0029）
	//   - ErrCategoryNotFound: 指定カテゴリがそのイベントの費用カテゴリに存在しない
	//   - ErrDuplicateCategory: 同一カテゴリが内訳で重複している
	Join(ctx context.Context, member *model.EventMember, notification model.JoinNotification) (JoinOutcome, error)

	// ConfirmJoin は確認トークンで未確定（pending）の申込を確定する（ADR-0044）。
	//
	// events を FOR UPDATE でロックして存在確認・取消状態・終了日時を確認し、tokenHash に
	// 一致する pending 行を確定する。確定時は confirmed の party_size 合計で定員を再確認する。
	// 申込期限は再確認しない（期限内に受け付けた申込をメール確認の遅れで失効させないため）。
	// 成功時は status='confirmed' にしてトークン列を NULL にし、確定日時を返す。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrEventCancelled: イベントが取りやめになっている
	//   - ErrEventEnded: end_date 経過後
	//   - ErrEventCapacityFull: 定員超過
	//   - ErrConfirmationTokenInvalid: トークンが不正・期限切れ・使用済み
	ConfirmJoin(ctx context.Context, eventID uuid.UUID, tokenHash string) (time.Time, error)

	// Leave はログイン参加者のイベント参加を1トランザクションで取り消す。
	//
	// events を FOR UPDATE でロックして存在確認・申込期限の取得を行い、event_members から
	// (event_id, profile_id) 一致行を DELETE し、同一トランザクション内で
	// event_participation_logs に action='leave' を1件追記する。参加取消とログ追記を
	// 原子的に行い、片方だけ成功する不整合を防ぐ。成功時は追記した leave ログの created_at を返す。
	// 匿名参加（profile_id NULL）は profile_id で識別できず、本メソッドの対象外。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrNotJoined: そのイベントに参加していない（削除対象行なし）
	//   - ErrDeadlinePassed: 申込期限ありイベントで期限経過後（期限後のキャンセルは欠席連絡 API を利用。ADR-0031）
	Leave(ctx context.Context, eventID, profileID uuid.UUID) (time.Time, error)

	// Absence はログイン参加者の欠席連絡を1トランザクションで受け付ける（ADR-0031）。
	//
	// events を FOR UPDATE でロックして存在確認・取消状態・申込期限・end_date の取得を行い、
	// event_members から (event_id, profile_id) 一致行を DELETE、event_participation_logs へ
	// action='absence'・reason・detail を1件追記、event_notification_outbox へ
	// recipient_kind='organizer' の通知予約（subject/body）を1件 INSERT する。すべて同一
	// トランザクション内で原子的に行い、追記した absence ログの created_at を返す。
	// detail が空文字の場合は participation_logs の detail には NULL を保存する。
	// 失敗時は次の sentinel エラーを %w でラップして返す:
	//   - ErrEventNotFound: イベントが存在しない
	//   - ErrNotJoined: そのイベントに参加していない（削除対象行なし）
	//   - ErrEventCancelled: イベントが取りやめになっている
	//   - ErrAbsenceBeforeDeadline: 申込期限ありイベントで期限前（期限前のキャンセルは leave を利用）
	//   - ErrEventEnded: end_date 経過後
	Absence(ctx context.Context, eventID, profileID uuid.UUID, reason, detail, subject, body string) (time.Time, error)

	// ListRecipients は指定した eventID に参加登録済みの宛先一覧を返す。
	// 未確定（pending）の申込は含めない（ADR-0044）。
	ListRecipients(ctx context.Context, eventID uuid.UUID) ([]model.EventRecipient, error)

	// ListMembers は指定 eventID の参加者一覧を作成日時の昇順で返す。
	// 未確定（pending）の申込は含めない（ADR-0044）。
	// profiles を LEFT JOIN してプロフィールサマリーを同時取得する。
	// 匿名参加（profile_id NULL）は EventMember.Profile が nil になる。
	// 0件の場合は nil ではなく空スライスを返す（呼び出し元の totalCount 計算で安全側に倒すため）。
	ListMembers(ctx context.Context, eventID uuid.UUID) ([]model.EventMember, error)

	// GetMemberByProfile は指定 eventID・profileID のログイン参加者の申込1件を返す。
	// 未確定（pending）の申込は対象外（ADR-0044）。
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

// Join はイベント参加を登録する（ADR-0044）。
// member.ProfileID が Invalid（匿名参加）の場合は NULL として保存される。
// 成功時は member.CreatedAt を INSERT の created_at で埋める。
func (r *eventJoinPostgres) Join(
	ctx context.Context,
	member *model.EventMember,
	notification model.JoinNotification,
) (JoinOutcome, error) {

	if member.Status != model.EventMemberStatusPending && member.Status != model.EventMemberStatusConfirmed {
		return "", fmt.Errorf("invalid member status %q", member.Status)
	}
	if member.Status == model.EventMemberStatusPending &&
		(member.ConfirmationTokenHash == "" || member.ConfirmationTokenExpiresAt.IsZero()) {
		return "", errors.New("pending join requires a confirmation token")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// イベント行をロックして存在確認・キャンセル状態・申込期限・終了日時の取得を同時に行う。
	// 同一イベントへの並行 join / confirm はこのロックで直列化される。
	const lockEvent = `
	SELECT capacity, cancelled_at, application_deadline, end_date
	FROM events
	WHERE id = $1
	FOR UPDATE
	`

	var (
		capacity    sql.NullInt32
		cancelledAt sql.NullTime
		deadline    sql.NullTime
		endDate     time.Time
	)
	err = tx.QueryRowContext(ctx, lockEvent, member.EventID).Scan(&capacity, &cancelledAt, &deadline, &endDate)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("event %s: %w", member.EventID, ErrEventNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("lock event: %w", err)
	}
	if cancelledAt.Valid {
		return "", fmt.Errorf("event %s: %w", member.EventID, ErrEventCancelled)
	}

	// 申込期限ありイベントで期限経過後の申込は拒否する。
	// 期限なし（NULL）は申込期限の概念がない。終了日時経過まで申し込める（ADR-0029）。
	if deadline.Valid && time.Now().After(deadline.Time) {
		return "", fmt.Errorf("event %s: %w", member.EventID, ErrDeadlinePassed)
	}
	// end_date 経過後の申込は拒否する。end_date は NOT NULL のため NULL 判定は不要。
	if time.Now().After(endDate) {
		return "", fmt.Errorf("event %s: %w", member.EventID, ErrEventEnded)
	}

	// 期限切れの未確定申込を同じイベントへの申込時に削除する（ADR-0044）。
	const deleteExpiredPending = `
	DELETE FROM event_members
	WHERE event_id = $1
	AND status = 'pending'
	AND confirmation_token_expires_at <= now()
	`
	if _, err := tx.ExecContext(ctx, deleteExpiredPending, member.EventID); err != nil {
		return "", fmt.Errorf("delete expired pending members: %w", err)
	}

	// 申込のカテゴリ名を、そのイベントの費用カテゴリ（event_costs）へ解決する。
	// 重複判定より先に行い、カテゴリ不正の応答を新規申込と重複申込で同じにする。
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
			return "", fmt.Errorf("category %q: %w", member.Categories[i].Category, ErrCategoryNotFound)
		}
		if err != nil {
			return "", fmt.Errorf("find cost: %w", err)
		}
		// 表記違いで同一カテゴリを指した場合もここで弾く（DB では PK 違反になる）。
		if _, dup := seenCost[costID]; dup {
			return "", fmt.Errorf("category %q: %w", member.Categories[i].Category, ErrDuplicateCategory)
		}
		seenCost[costID] = struct{}{}

		member.Categories[i].CostID = costID
		// 保存・返却するカテゴリ名は event_costs 側の表記に揃える。
		member.Categories[i].Category = category
	}

	// ログイン中の本人が既に参加している場合は、自分の申込の重複として扱う。
	if member.ProfileID.Valid {
		const existsProfileMember = `
		SELECT EXISTS(
			SELECT 1
			FROM event_members
			WHERE event_id = $1
			AND profile_id = $2
		)
		`
		var joined bool
		if err := tx.QueryRowContext(
			ctx,
			existsProfileMember,
			member.EventID,
			member.ProfileID,
		).Scan(&joined); err != nil {
			return "", fmt.Errorf("exists profile member: %w", err)
		}
		if joined {
			return JoinOutcomeAlreadyJoinedSelf, nil
		}
	}

	// 同一メールアドレスの申込を確認する。mail_address は UNIQUE インデックス
	// （lower(mail_address)）と同じ基準で比較する。
	const findMemberByMail = `
	SELECT id, profile_id, status
	FROM event_members
	WHERE event_id = $1
	AND lower(mail_address) = lower($2)
	`

	var (
		duplicateID        uuid.UUID
		duplicateProfileID uuid.NullUUID
		duplicateStatus    string
	)
	duplicateFound := true
	err = tx.QueryRowContext(
		ctx,
		findMemberByMail,
		member.EventID,
		member.MailAddress,
	).Scan(&duplicateID, &duplicateProfileID, &duplicateStatus)
	if errors.Is(err, sql.ErrNoRows) {
		duplicateFound = false
	} else if err != nil {
		return "", fmt.Errorf("find member by mail: %w", err)
	}

	// 定員確認。capacity が NULL または 0 は「定員なし」。
	// 人数は confirmed の party_size 合計で数える（未確定は含めない。ADR-0044）。
	// 重複申込でも新規申込と同じ条件で判定し、応答の違いで申込の有無が分からないようにする。
	capacityFull := false
	if capacity.Valid && capacity.Int32 > 0 {
		const sumPartySize = `
		SELECT COALESCE(SUM(party_size), 0)
		FROM event_members
		WHERE event_id = $1
		AND status = 'confirmed'
		`

		var taken int
		if err := tx.QueryRowContext(ctx, sumPartySize, member.EventID).Scan(&taken); err != nil {
			return "", fmt.Errorf("sum party size: %w", err)
		}
		capacityFull = taken+member.PartySize > int(capacity.Int32)
	}

	if capacityFull {
		if duplicateFound {
			if err := insertJoinOutboxEmail(ctx, tx, member, notification.NoticeSubject, notification.NoticeBody); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit transaction: %w", err)
		}
		return JoinOutcomeCapacityFull, nil
	}

	if duplicateFound {
		// ログイン参加で同一メールの pending 行がある場合は、その未確定行を破棄して
		// ログイン申込で置き換える（第三者が作った pending 行で本人をブロックしない）。
		if member.ProfileID.Valid && duplicateStatus == model.EventMemberStatusPending && !duplicateProfileID.Valid {
			const deletePending = `DELETE FROM event_members WHERE id = $1`
			if _, err := tx.ExecContext(ctx, deletePending, duplicateID); err != nil {
				return "", fmt.Errorf("delete pending member: %w", err)
			}
		} else {
			if err := insertJoinOutboxEmail(ctx, tx, member, notification.NoticeSubject, notification.NoticeBody); err != nil {
				return "", err
			}
			if err := tx.Commit(); err != nil {
				return "", fmt.Errorf("commit transaction: %w", err)
			}
			return JoinOutcomeAlreadyJoined, nil
		}
	}

	const insertMember = `
	INSERT INTO event_members(
		id,
		event_id,
		profile_id,
		username,
		mail_address,
		party_size,
		status,
		confirmation_token_hash,
		confirmation_token_expires_at
	)
	VALUES(
		gen_random_uuid(),
		$1,
		$2,
		$3,
		$4,
		$5,
		$6,
		$7,
		$8
	)
	RETURNING id, created_at
	`

	var (
		tokenHash    any
		tokenExpires any
	)
	if member.Status == model.EventMemberStatusPending {
		tokenHash = member.ConfirmationTokenHash
		tokenExpires = member.ConfirmationTokenExpiresAt
	}

	err = tx.QueryRowContext(
		ctx,
		insertMember,
		member.EventID,
		member.ProfileID,
		member.Username,
		member.MailAddress,
		member.PartySize,
		member.Status,
		tokenHash,
		tokenExpires,
	).Scan(&member.ID, &member.CreatedAt)
	if err != nil {
		// UNIQUE 制約違反は重複参加として扱う（事前チェックの最後の砦）。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
			switch pgErr.ConstraintName {
			case "event_members_event_id_lower_mail_key":
				return JoinOutcomeAlreadyJoined, nil
			case "event_members_event_id_profile_id_key":
				return JoinOutcomeAlreadyJoinedSelf, nil
			}
		}
		return "", fmt.Errorf("join event: %w", err)
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
			return "", fmt.Errorf("insert member category: %w", err)
		}
	}

	if member.Status == model.EventMemberStatusPending {
		// 確認メールを outbox に予約する。送信はワーカーが非同期で行う（ADR-0044）。
		if err := insertJoinOutboxEmail(
			ctx, tx, member, notification.ConfirmationSubject, notification.ConfirmationBody,
		); err != nil {
			return "", err
		}
	} else if member.ProfileID.Valid {
		// ログイン参加時のみ、参加状態ログに join を追記する（同一トランザクション内で原子的に）。
		// event_participation_logs.profile_id は NOT NULL のため、匿名参加（profile_id NULL）は
		// ログ記録の対象外とする。参加登録とログ追記を1トランザクションにまとめることで、
		// 片方だけ成功する不整合を防ぐ。
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
			return "", fmt.Errorf("insert participation log: %w", err)
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
				return "", fmt.Errorf("insert participation log category: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	if member.Status == model.EventMemberStatusPending {
		return JoinOutcomePending, nil
	}
	return JoinOutcomeConfirmed, nil
}

// insertJoinOutboxEmail は参加申込のメールを outbox に予約する（ADR-0044）。
// subject が空の場合は何もしない（リポジトリ単体テストで文面を省略できるようにするため）。
func insertJoinOutboxEmail(
	ctx context.Context,
	tx *sql.Tx,
	member *model.EventMember,
	subject, body string,
) error {
	if subject == "" {
		return nil
	}

	const query = `
	INSERT INTO event_notification_outbox(
		event_id,
		recipient_kind,
		recipient_email,
		subject,
		body
	)
	VALUES($1, $2, $3, $4, $5)
	`

	if _, err := tx.ExecContext(
		ctx,
		query,
		member.EventID,
		model.NotificationOutboxRecipientKindEmail,
		member.MailAddress,
		subject,
		body,
	); err != nil {
		return fmt.Errorf("insert join notification outbox: %w", err)
	}
	return nil
}

// ConfirmJoin は確認トークンで未確定（pending）の申込を確定する（ADR-0044）。
// events を FOR UPDATE でロックして存在確認・取消状態・終了日時を確認し、
// tokenHash に一致する pending 行を確定する。定員は confirmed の合計で再確認する。
// 申込期限は再確認しない（期限内に受け付けた申込をメール確認の遅れで失効させないため）。
func (r *eventJoinPostgres) ConfirmJoin(
	ctx context.Context,
	eventID uuid.UUID,
	tokenHash string,
) (time.Time, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const lockEvent = `
	SELECT capacity, cancelled_at, end_date
	FROM events
	WHERE id = $1
	FOR UPDATE
	`

	var (
		capacity    sql.NullInt32
		cancelledAt sql.NullTime
		endDate     time.Time
	)
	err = tx.QueryRowContext(ctx, lockEvent, eventID).Scan(&capacity, &cancelledAt, &endDate)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock event: %w", err)
	}
	if cancelledAt.Valid {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventCancelled)
	}
	if time.Now().After(endDate) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventEnded)
	}

	const deleteExpiredPending = `
	DELETE FROM event_members
	WHERE event_id = $1
	AND status = 'pending'
	AND confirmation_token_expires_at <= now()
	`
	if _, err := tx.ExecContext(ctx, deleteExpiredPending, eventID); err != nil {
		return time.Time{}, fmt.Errorf("delete expired pending members: %w", err)
	}

	const findPending = `
	SELECT id, party_size
	FROM event_members
	WHERE event_id = $1
	AND confirmation_token_hash = $2
	AND status = 'pending'
	AND confirmation_token_expires_at > now()
	FOR UPDATE
	`

	var (
		memberID  uuid.UUID
		partySize int
	)
	err = tx.QueryRowContext(ctx, findPending, eventID, tokenHash).Scan(&memberID, &partySize)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrConfirmationTokenInvalid)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("find pending member: %w", err)
	}

	if capacity.Valid && capacity.Int32 > 0 {
		const sumPartySize = `
		SELECT COALESCE(SUM(party_size), 0)
		FROM event_members
		WHERE event_id = $1
		AND status = 'confirmed'
		`

		var taken int
		if err := tx.QueryRowContext(ctx, sumPartySize, eventID).Scan(&taken); err != nil {
			return time.Time{}, fmt.Errorf("sum party size: %w", err)
		}
		if taken+partySize > int(capacity.Int32) {
			return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventCapacityFull)
		}
	}

	const confirmMember = `
	UPDATE event_members
	SET status = 'confirmed',
	    confirmation_token_hash = NULL,
	    confirmation_token_expires_at = NULL
	WHERE id = $1
	RETURNING now()
	`

	var confirmedAt time.Time
	if err := tx.QueryRowContext(ctx, confirmMember, memberID).Scan(&confirmedAt); err != nil {
		return time.Time{}, fmt.Errorf("confirm member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, fmt.Errorf("commit transaction: %w", err)
	}
	return confirmedAt, nil
}

// Leave はログイン参加者のイベント参加を取り消す。
// events を FOR UPDATE でロックして存在確認と申込期限の取得を行い、event_members から
// (event_id, profile_id) 一致行を DELETE し、同一トランザクション内で
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

	// イベント行をロックして存在確認と申込期限の取得を同時に行う。
	const lockEvent = `
	SELECT application_deadline
	FROM events
	WHERE id = $1
	FOR UPDATE
	`

	var deadline sql.NullTime
	err = tx.QueryRowContext(ctx, lockEvent, eventID).Scan(&deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock event: %w", err)
	}

	// 申込期限ありイベントで期限経過後のキャンセルは拒否する。
	// 期限なし（NULL）は期限の概念がないため常時可（ADR-0031）。
	if deadline.Valid && time.Now().After(deadline.Time) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrDeadlinePassed)
	}

	// 参加行を削除する。ログイン参加者は1イベントにつき高々1行のため、
	// (event_id, profile_id) で一意に対象を特定できる。
	// イベント存在は手前で確認済みのため、削除対象なしは未参加のみ。
	// 未確定（pending）の行は profile_id を持たないが、防御的に確定済みだけを対象にする。
	const deleteMember = `
	DELETE FROM event_members
	WHERE event_id = $1
	AND profile_id = $2
	AND status = 'confirmed'
	`

	res, err := tx.ExecContext(ctx, deleteMember, eventID, profileID)
	if err != nil {
		return time.Time{}, fmt.Errorf("delete member: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return time.Time{}, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
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

// Absence はログイン参加者の欠席連絡を受け付ける（ADR-0031）。
// events の状態確認 → event_members の該当行 DELETE → event_participation_logs へ
// action='absence'・reason・detail の追記 → event_notification_outbox へ
// recipient_kind='organizer' の通知予約、を1トランザクションで原子的に行い、
// 追記した absence ログの created_at を返す。
func (r *eventJoinPostgres) Absence(
	ctx context.Context,
	eventID, profileID uuid.UUID,
	reason, detail, subject, body string,
) (time.Time, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// イベント行をロックして存在確認・キャンセル状態・申込期限・終了日時の取得を同時に行う。
	const lockEvent = `
	SELECT cancelled_at, application_deadline, end_date
	FROM events
	WHERE id = $1
	FOR UPDATE
	`

	var (
		cancelledAt sql.NullTime
		deadline    sql.NullTime
		endDate     time.Time
	)
	err = tx.QueryRowContext(ctx, lockEvent, eventID).Scan(&cancelledAt, &deadline, &endDate)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock event: %w", err)
	}
	if cancelledAt.Valid {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventCancelled)
	}

	now := time.Now()
	// 申込期限ありイベントで期限前の欠席連絡は拒否する（期限前のキャンセルは leave を利用。
	// ADR-0031）。期限ちょうど（now == deadline）は期限経過として受付可。
	if deadline.Valid && now.Before(deadline.Time) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrAbsenceBeforeDeadline)
	}
	// end_date 経過後の欠席連絡は拒否する。end_date は NOT NULL のため NULL 判定は不要。
	if now.After(endDate) {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventEnded)
	}

	// 参加行を削除する。ログイン参加者は1イベントにつき高々1行のため、
	// (event_id, profile_id) で一意に対象を特定できる。
	// イベント存在は手前で確認済みのため、削除対象なしは未参加のみ。
	// 未確定（pending）の行は profile_id を持たないが、防御的に確定済みだけを対象にする。
	const deleteMember = `
	DELETE FROM event_members
	WHERE event_id = $1
	AND profile_id = $2
	AND status = 'confirmed'
	`

	res, err := tx.ExecContext(ctx, deleteMember, eventID, profileID)
	if err != nil {
		return time.Time{}, fmt.Errorf("delete member: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return time.Time{}, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrNotJoined)
	}

	// 欠席連絡と同時に、参加状態ログへ reason・detail 付きの absence を追記する
	// （同一トランザクション内で原子的に）。reason・detail は空文字の場合 NULL として保存する。
	const insertParticipationLog = `
	INSERT INTO event_participation_logs(
		event_id,
		profile_id,
		action,
		reason,
		detail
	)
	VALUES($1, $2, 'absence', $3, $4)
	RETURNING created_at
	`

	reasonCol := sql.NullString{}
	if reason != "" {
		reasonCol = sql.NullString{String: reason, Valid: true}
	}

	detailCol := sql.NullString{}
	if detail != "" {
		detailCol = sql.NullString{String: detail, Valid: true}
	}

	var createdAt time.Time
	if err := tx.QueryRowContext(
		ctx,
		insertParticipationLog,
		eventID,
		profileID,
		reasonCol,
		detailCol,
	).Scan(&createdAt); err != nil {
		return time.Time{}, fmt.Errorf("insert participation log: %w", err)
	}

	// 主催者宛ての欠席連絡メールを outbox に予約する（Transactional Outbox パターン。ADR-0031）。
	// 宛先は送信直前にワーカーが events JOIN profiles から解決する。
	const insertOutbox = `
	INSERT INTO event_notification_outbox(
		event_id,
		recipient_kind,
		subject,
		body
	)
	VALUES($1, $2, $3, $4)
	`

	if _, err := tx.ExecContext(
		ctx,
		insertOutbox,
		eventID,
		model.NotificationOutboxRecipientKindOrganizer,
		subject,
		body,
	); err != nil {
		return time.Time{}, fmt.Errorf("insert notification outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, fmt.Errorf("commit transaction: %w", err)
	}

	return createdAt, nil
}

// ListRecipients は指定した eventID に参加登録済みの宛先一覧を返す。
// 未確定（pending）の申込は通知対象に含めない（ADR-0044）。
func (r *eventJoinPostgres) ListRecipients(ctx context.Context, eventID uuid.UUID) ([]model.EventRecipient, error) {
	// 参加登録順で返す（送信順を決定的にし、ログ・監査での追跡を容易にする）。
	const query = `
	SELECT mail_address
	FROM event_members
	WHERE event_id = $1
	AND status = 'confirmed'
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
// 未確定（pending）の申込は参加者一覧に含めない（ADR-0044）。
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
	AND m.status = 'confirmed'
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
// 申込行と内訳は LEFT JOIN の1クエリで取得する（ADR-0026）。
// 未確定（pending）の申込は対象外（ADR-0044）。
func (r *eventJoinPostgres) GetMemberByProfile(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.EventMember, error) {
	const query = `
	SELECT m.id, m.username, m.mail_address, m.party_size, m.created_at,
	       mc.cost_id, c.category, mc.head_count
	FROM event_members m
	LEFT JOIN event_member_categories mc ON mc.member_id = m.id
	LEFT JOIN event_costs c ON c.id = mc.cost_id
	WHERE m.event_id = $1
	AND m.profile_id = $2
	AND m.status = 'confirmed'
	ORDER BY c.category
	`

	rows, err := r.db.QueryContext(ctx, query, eventID, profileID)
	if err != nil {
		return model.EventMember{}, fmt.Errorf("get member by profile: %w", err)
	}
	defer func() { _ = rows.Close() }()

	member := model.EventMember{
		EventID:   eventID,
		ProfileID: uuid.NullUUID{UUID: profileID, Valid: true},
	}
	// 0件でも空スライスを返す（呼び出し元の Participants を null ではなく [] にするため）。
	categories := []model.MemberCategory{}
	found := false

	// 申込行の列は内訳の件数ぶん重複して返るため、組み立てるのは1行目だけにする。
	for rows.Next() {
		var (
			id          uuid.UUID
			username    string
			mailAddress string
			partySize   int
			createdAt   time.Time
			costID      uuid.NullUUID
			category    sql.NullString
			headCount   sql.NullInt32
		)
		if err := rows.Scan(
			&id,
			&username,
			&mailAddress,
			&partySize,
			&createdAt,
			&costID,
			&category,
			&headCount,
		); err != nil {
			return model.EventMember{}, fmt.Errorf("scan member: %w", err)
		}

		if !found {
			member.ID = id
			member.Username = username
			member.MailAddress = mailAddress
			member.PartySize = partySize
			member.CreatedAt = createdAt
			found = true
		}

		// 内訳0件の申込は LEFT JOIN により1行返り、cost_id 等が NULL になる。
		if costID.Valid {
			categories = append(categories, model.MemberCategory{
				CostID:    costID.UUID,
				Category:  category.String,
				HeadCount: int(headCount.Int32),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return model.EventMember{}, fmt.Errorf("get member by profile rows: %w", err)
	}

	if !found {
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
	JOIN event_members m ON m.id = mc.member_id
	JOIN event_costs c ON c.id = mc.cost_id
	WHERE mc.event_id = $1
	AND m.status = 'confirmed'
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
