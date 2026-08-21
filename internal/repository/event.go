package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/unicode/norm"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ErrTagNotFound は紐づけ対象のタグが存在しない場合に返されるエラー。
var ErrTagNotFound = errors.New("tag not found")

// ErrEventAlreadyCancelled は既にキャンセル済みのイベントを再度キャンセルしようとした
// 場合に返されるエラー。CancelWithNotification は非冪等（毎回リクエストごとに参加者への
// 通知文面を受け取り outbox に予約する）のため、2回目以降の呼び出しは失敗として扱う。
var ErrEventAlreadyCancelled = errors.New("event already cancelled")

// nullInt32 は 0 を NULL として扱う（未設定を表す）。
// capacity は定員数であり int32 の範囲内であることが仕様上保証されているため変換する。
func nullInt32(n int) sql.NullInt32 {
	if n == 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(n), Valid: true} //nolint:gosec
}

// nullTime はゼロ値を NULL として扱う（未設定を表す）。
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// filenameAt は names[i] を返す。範囲外なら空文字を返す（ファイル名は任意のため）。
// 画像・PDF のオブジェクトキーと元ファイル名を同順で対応付ける際に使う。
func filenameAt(names []string, i int) string {
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return ""
}

// EventRepository は events テーブルへのアクセスを抽象化する。
type EventRepository interface {
	// ListSummaries は指定されたソート順でイベントサマリーを取得する。
	// sort は "created_at" または "event_date"、order は "asc" または "desc"。
	// 同一ソートキーのレコードは id 昇順で安定ソートする。
	ListSummaries(ctx context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountSummaries は events テーブルの全件数を返す。
	CountSummaries(ctx context.Context) (int, error)
	// SearchSummaries は filter に一致するイベントサマリーを指定ソート順で取得する。
	// 各キーワードは title/description/location/主催者名(display_name)/持ち物(event_item)
	// を横断（OR）し、キーワード間は AND で結合する（AND 検索）。
	// タグは複数指定時 OR（いずれかを持てば該当）、開催状況(status)も複数指定時 OR（ADR-0027）、
	// 地域(location)は e.location への部分一致で複数指定時 OR（ADR-0028）で、
	// キーワード条件・タグ条件・開催状況条件・地域条件は互いに AND で結合する。
	// filter は条件を 1 つ以上含むことを前提とする（IsEmpty() が false）。
	SearchSummaries(ctx context.Context, filter model.EventSearchFilter, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountSearchSummaries は filter に一致するイベントの件数を返す。
	// filter は条件を 1 つ以上含むことを前提とする（IsEmpty() が false）。
	CountSearchSummaries(ctx context.Context, filter model.EventSearchFilter) (int, error)
	// ListMySummaries は filter（プロフィール＋種別）に一致するイベントサマリーを指定ソート順で取得する。
	// 種別ごとの絞り込み条件は myEventClauses を参照（種別の定義は ADR-0024）。
	// filter.Kind は呼び出し元（service 層）で検証済みであることを前提とする。
	ListMySummaries(ctx context.Context, filter model.MyEventFilter, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountMyEventKinds は指定プロフィールの種別ごとの件数を1クエリで返す。
	// profileID はパース済みの uuid.UUID を受け取り、正規化文字列でクエリする。
	CountMyEventKinds(ctx context.Context, profileID uuid.UUID) (model.MyEventCounts, error)
	// GetByID は指定されたイベント ID の詳細情報を取得する。
	GetByID(ctx context.Context, id string) (*model.EventResponse, error)
	// Create はイベントを関連テーブルとともにトランザクション内で一括登録する。
	Create(ctx context.Context, e *model.NewEvent) (model.CreateEventResponse, error)
	// GetOwnerProfileID は指定した eventID のイベント投稿者 profile_id を返す。
	// イベントが存在しない場合は sql.ErrNoRows を %w でラップして返す。
	GetOwnerProfileID(ctx context.Context, eventID string) (string, error)
	// GetTitle は指定した eventID のイベントタイトルを返す。
	// イベントが存在しない場合は ErrEventNotFound を %w でラップして返す。
	GetTitle(ctx context.Context, eventID string) (string, error)
	// Exists は指定した eventID のイベントが存在するかを返す。
	// 存在しない場合は (false, nil)、それ以外のエラーは %w でラップして返す。
	// eventID はパース済みの uuid.UUID を受け取り、正規化文字列でクエリする。
	Exists(ctx context.Context, eventID uuid.UUID) (bool, error)
	// CancelWithNotification は指定したイベントを取りやめ状態にし、同一トランザクション内で
	// 参加者への通知メール（subject/body）を event_notification_outbox に1件予約する
	// （Transactional Outbox パターン）。イベントのキャンセル確定と通知予約を原子的に行う。
	//
	// 非冪等: 既にキャンセル済みのイベントに対して呼び出した場合は
	// ErrEventAlreadyCancelled を %w でラップして返し、outbox への予約も行わない。
	// イベントが存在しない場合は ErrEventNotFound を %w でラップして返す。
	CancelWithNotification(ctx context.Context, eventID uuid.UUID, subject, body string) (cancelledAt time.Time, err error)
}

// eventPostgres は EventRepository の PostgreSQL 実装。
type eventPostgres struct {
	db *sql.DB
}

// NewEventRepository は *sql.DB を使う EventRepository を生成する。
func NewEventRepository(db *sql.DB) EventRepository {
	return &eventPostgres{db: db}
}

// eventSummarySelect は一覧系クエリが共通で使う SELECT 句。
// 一覧表示に必要なカラムのみ取得し、description / external_url / capacity / updated_at は含めない。
// 列の並びは scanEventSummaries の Scan 順と対応する。
const eventSummarySelect = `
	SELECT e.id, e.title, e.event_date, e.end_date, e.application_deadline, e.location, e.profile_id, e.cancelled_at, e.created_at,
	       p.id, p.display_name, p.avatar_url
	FROM events e
	LEFT JOIN profiles p ON p.id = e.profile_id`

// scanEventSummaries は eventSummarySelect の列順で rows を読み出す。
// レコードが 0 件でも nil ではなく空スライスを返す。
func scanEventSummaries(rows *sql.Rows) ([]model.EventSummary, error) {
	var summaries []model.EventSummary
	for rows.Next() {
		var s model.EventSummary
		var (
			location            sql.NullString
			profileID           sql.NullString
			cancelledAt         sql.NullTime
			applicationDeadline sql.NullTime
			pID                 sql.NullString
			displayName         sql.NullString
			avatarURL           sql.NullString
		)
		if err := rows.Scan(
			&s.ID,
			&s.Title,
			&s.EventDate,
			&s.EndDate,
			&applicationDeadline,
			&location,
			&profileID,
			&cancelledAt,
			&s.CreatedAt,
			&pID,
			&displayName,
			&avatarURL,
		); err != nil {
			return nil, fmt.Errorf("scan event summary: %w", err)
		}
		s.Location = location.String
		s.ProfileID = profileID.String
		if cancelledAt.Valid {
			s.CancelledAt = &cancelledAt.Time
		}
		if applicationDeadline.Valid {
			s.ApplicationDeadline = &applicationDeadline.Time
		}
		s.Profile = model.ProfileSummary{
			ID:          pID.String,
			DisplayName: displayName.String,
			AvatarURL:   avatarURL.String,
		}
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event summaries: %w", err)
	}

	if summaries == nil {
		summaries = []model.EventSummary{}
	}
	return summaries, nil
}

// ListSummaries は全イベントのサマリーを指定ソート順で取得する。
// sort・order は呼び出し元（service 層）でホワイトリスト検証済みであることを前提とする。
func (r *eventPostgres) ListSummaries(ctx context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	// G201: 埋め込むのはホワイトリスト由来の ORDER BY のみ。limit / offset は args 経由で渡す。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`%s
		ORDER BY %s
		LIMIT $1 OFFSET $2`, eventSummarySelect, orderByClause(sort, order))

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list event summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries, err := scanEventSummaries(rows)
	if err != nil {
		return nil, err
	}
	if err := r.attachTagsToSummaries(ctx, summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

// attachTagsToSummaries は summaries に紐づくタグを1クエリで一括取得し、
// 各 summary の Tags フィールドに割り当てる（N+1 回避）。
// タグが無いイベントは Tags を nil のままにし、JSON では omitempty で省略させる。
func (r *eventPostgres) attachTagsToSummaries(ctx context.Context, summaries []model.EventSummary) error {
	// 空スライスだと WHERE IN () が構文エラーになるため早期 return する（この判定は消さない）。
	if len(summaries) == 0 {
		return nil
	}

	placeholders := make([]string, len(summaries))
	args := make([]any, len(summaries))
	for i, s := range summaries {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s.ID
	}

	// G201: 埋め込むのは summaries の件数ぶんのプレースホルダ番号($N)のみ。
	// event_id の実値は args 経由でのみ渡すため SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`
		SELECT et.event_id, t.id, t.name
		FROM event_tags et
		JOIN tags t ON t.id = et.tag_id
		WHERE et.event_id IN (%s)
		ORDER BY t.name ASC`, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list event tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tagsByEvent := make(map[string][]model.TagResponse)
	for rows.Next() {
		var eventID string
		var tag model.TagResponse
		if err := rows.Scan(&eventID, &tag.ID, &tag.Name); err != nil {
			return fmt.Errorf("scan event tag: %w", err)
		}
		tagsByEvent[eventID] = append(tagsByEvent[eventID], tag)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate event tags: %w", err)
	}

	for i := range summaries {
		summaries[i].Tags = tagsByEvent[summaries[i].ID]
	}
	return nil
}

// CountSummaries は events テーブルの全件数を返す。
func (r *eventPostgres) CountSummaries(ctx context.Context) (int, error) {
	const query = `SELECT COUNT(*) FROM events`

	var count int
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count event summaries: %w", err)
	}
	return count, nil
}

// escapeLike は ILIKE のワイルドカード(% _)とエスケープ文字(\)を無効化し、
// ユーザー入力を純粋な部分一致文字列として扱う。PostgreSQL の ILIKE は
// デフォルトのエスケープ文字が \ のため ESCAPE 句は不要。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// eventOrderByClauses は (sort, order) の組み合わせから安全な ORDER BY 句へのマップ。
// ユーザー入力を直接 SQL に埋め込まず、ホワイトリストから固定文字列を選ぶ。
// 同一ソートキーは id 昇順で安定ソートする。
var eventOrderByClauses = map[string]string{
	"event_date:asc":  "e.event_date ASC, e.id",
	"event_date:desc": "e.event_date DESC, e.id",
	"created_at:asc":  "e.created_at ASC, e.id",
	"created_at:desc": "e.created_at DESC, e.id",
}

// orderByClause は (sort, order) に対応する ORDER BY 句を返す。
// 未知の組み合わせは created_at DESC にフォールバックする
// （service 層で正規化済みのため通常到達しない）。
func orderByClause(sort, order string) string {
	if clause, ok := eventOrderByClauses[sort+":"+order]; ok {
		return clause
	}
	return eventOrderByClauses["created_at:desc"]
}

// NormalizeSearchText は照合基準を全角/半角で揃えるため NFKC 正規化する。
// 全角数字→半角数字、全角英字→半角英字、半角カナ→全角カナ 等を吸収する
// （ひらがな↔カタカナは対象外）。SQL 側の normalize(col, NFKC) と同一の正規化形を用いることで、
// 保存値とキーワードの表記ゆれ（半角/全角）を一致させる。
//
// service 層（重複除去キーの生成）と repository 層（ILIKE 用パターンの生成）は
// 同じ正規化形を共有する（ADR-0028）。
func NormalizeSearchText(s string) string {
	return norm.NFKC.String(s)
}

// statusClauses は開催状況(status)の値 → SQL 条件式の対応（ADR-0027）。
// ongoing は AND を含むため、他の条件と OR で連結しても優先順位が崩れないよう括弧で囲む。
var statusClauses = map[model.EventStatus]string{
	model.EventStatusUpcoming: "e.event_date > now()",
	model.EventStatusOngoing:  "(e.event_date <= now() AND e.end_date >= now())",
	model.EventStatusEnded:    "e.end_date < now()",
}

// buildSearchWhere は filter の条件を AND で結合した WHERE 句本体とその引数を返す。
//
// キーワードは1語につき5フィールド(title/description/display_name/location/持ち物)を OR で
// 横断する1グループとなり、グループ間は AND で連結する（全語に一致するものだけが該当）。
// タグは1つの EXISTS にまとめ、tag_id を IN で並べることで OR（いずれかのタグを持てば該当）とする。
// 開催状況(status)は statusClauses の条件式を OR で連結した1グループにする。
// 地域(location)は e.location 単独への ILIKE 条件を OR で連結した1グループにする（ADR-0028）。
// キーワード条件・タグ条件・開催状況条件・地域条件は互いに AND で結合する。
//
// プレースホルダは $startIdx から連番で割り当てる。キーワード・タグID・地域は常にプレースホルダ
// 経由で渡し、SQL 文字列へ直接埋め込まない（SQLインジェクション対策）。開催状況は statusClauses
// というホワイトリスト由来の定数文字列のみを使うため、プレースホルダを消費しない。
// 半角/全角を同一視するため、カラム側は normalize(col, NFKC)、キーワード・地域側は
// NormalizeSearchText で NFKC 正規化する（両辺を同じ正規化形にそろえる）。
//
// filter は条件を 1 つ以上含むことを前提とする（0 件だと空の WHERE となり不正な SQL になる）。
// filter.Statuses は statusClauses に存在する値のみを含むことを前提とする
// （未知の値が含まれる場合はエラーを返す）。
func buildSearchWhere(filter model.EventSearchFilter, startIdx int) (string, []any, error) {
	conds := make([]string, 0, len(filter.Keywords)+3)
	args := make([]any, 0, len(filter.Keywords)+len(filter.TagIDs)+len(filter.Locations))

	for _, kw := range filter.Keywords {
		ph := fmt.Sprintf("$%d", startIdx+len(args))
		// %[1]s は同一プレースホルダを5箇所へ展開する（ワイヤプロトコル上、同一 $N の複数参照は正当）。
		conds = append(conds, fmt.Sprintf(
			"(normalize(e.title, NFKC) ILIKE %[1]s OR normalize(e.description, NFKC) ILIKE %[1]s "+
				"OR normalize(p.display_name, NFKC) ILIKE %[1]s OR normalize(e.location, NFKC) ILIKE %[1]s "+
				"OR EXISTS (SELECT 1 FROM event_items it WHERE it.event_id = e.id "+
				"AND normalize(it.event_item, NFKC) ILIKE %[1]s))",
			ph,
		))
		// NFKC 正規化 → LIKE エスケープ → % で囲む の順。全角％(U+FF05)は NFKC で ASCII '%' に
		// なるため、正規化を先に行い escapeLike でワイルドカードとして無効化する必要がある。
		args = append(args, "%"+escapeLike(NormalizeSearchText(kw))+"%")
	}

	if len(filter.TagIDs) > 0 {
		placeholders := make([]string, len(filter.TagIDs))
		for i, tagID := range filter.TagIDs {
			placeholders[i] = fmt.Sprintf("$%d", startIdx+len(args))
			args = append(args, tagID)
		}
		// EXISTS の相関サブクエリにすることで、タグを複数指定しても外側の行は重複しない。
		// （JOIN で展開すると1イベントがタグ数ぶん重複し COUNT(*) が狂う）
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM event_tags et WHERE et.event_id = e.id AND et.tag_id IN (%s))",
			strings.Join(placeholders, ", "),
		))
	}

	if len(filter.Statuses) > 0 {
		statusConds := make([]string, len(filter.Statuses))
		for i, status := range filter.Statuses {
			clause, ok := statusClauses[status]
			if !ok {
				return "", nil, fmt.Errorf("build search where: unknown status %q", status)
			}
			statusConds[i] = clause
		}
		conds = append(conds, "("+strings.Join(statusConds, " OR ")+")")
	}

	if len(filter.Locations) > 0 {
		locConds := make([]string, len(filter.Locations))
		for i, loc := range filter.Locations {
			ph := fmt.Sprintf("$%d", startIdx+len(args))
			locConds[i] = fmt.Sprintf("normalize(e.location, NFKC) ILIKE %s", ph)
			// NFKC 正規化 → LIKE エスケープ → % で囲む の順（キーワードと同じ。ADR-0028）。
			args = append(args, "%"+escapeLike(NormalizeSearchText(loc))+"%")
		}
		conds = append(conds, "("+strings.Join(locConds, " OR ")+")")
	}

	return strings.Join(conds, " AND "), args, nil
}

// SearchSummaries は filter に一致するイベントサマリーを指定ソート順で取得する。
// 各キーワードは title/description/location/主催者名(display_name)/持ち物(event_item) を横断（OR）し、
// キーワード間は AND で結合する。タグ・開催状況(status)・地域(location)は複数指定時 OR で、
// キーワード条件・タグ条件・開催状況条件・地域条件は互いに AND で結合する（ADR-0027, ADR-0028）。
// sort・order は呼び出し元（service 層）でホワイトリスト検証済みであることを前提とする。
func (r *eventPostgres) SearchSummaries(ctx context.Context, filter model.EventSearchFilter, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	where, args, err := buildSearchWhere(filter, 1)
	if err != nil {
		return nil, fmt.Errorf("search event summaries: %w", err)
	}

	// limit / offset はキーワード分のプレースホルダの後ろに割り当てる。
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	// G201: 埋め込むのは buildSearchWhere が生成する列名+プレースホルダ番号($N)、
	// statusClauses 由来の固定文字列、ホワイトリスト由来の ORDER BY、int のインデックスのみ。
	// キーワード・タグID等のユーザー入力は一切文字列連結せず args 経由でのみ渡すため
	// SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`%s
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, eventSummarySelect, where, orderByClause(sort, order), limitIdx, offsetIdx)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search event summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries, err := scanEventSummaries(rows)
	if err != nil {
		return nil, err
	}
	if err := r.attachTagsToSummaries(ctx, summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

// CountSearchSummaries は filter に一致するイベントの件数を返す。
// LEFT JOIN profiles は 1 対 1、持ち物とタグは EXISTS のため行の重複は起きず COUNT(*) で正しい。
func (r *eventPostgres) CountSearchSummaries(ctx context.Context, filter model.EventSearchFilter) (int, error) {
	where, args, err := buildSearchWhere(filter, 1)
	if err != nil {
		return 0, fmt.Errorf("count search event summaries: %w", err)
	}

	// G201: 埋め込むのは buildSearchWhere が生成する列名+プレースホルダ番号($N)と
	// statusClauses 由来の固定文字列のみ。キーワード・タグIDは args 経由でのみ渡すため
	// SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		WHERE %s`, where)

	var count int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count search event summaries: %w", err)
	}
	return count, nil
}

// myEventClause は種別ごとの JOIN と WHERE を保持する。プレースホルダ $1 は profile_id。
type myEventClause struct {
	join  string
	where string
}

// myEventClauses は種別ごとの絞り込み条件。一覧（ListMySummaries）と
// 件数（CountMyEventKinds）の両方がこの定義を参照する。
//
// applied / attended は event_members に自分の行がある（＝現在申込中の）イベントだけを対象とする。
// 参加キャンセル（leave）で行が削除されたイベントはどちらにも現れない。
// 種別の定義と設計判断は ADR-0024 を参照。
var myEventClauses = map[model.MyEventKind]myEventClause{
	model.MyEventKindHosted: {
		where: "e.profile_id = $1",
	},
	model.MyEventKindApplied: {
		join:  "JOIN event_members m ON m.event_id = e.id AND m.profile_id = $1",
		where: "e.end_date >= now()",
	},
	model.MyEventKindAttended: {
		join:  "JOIN event_members m ON m.event_id = e.id AND m.profile_id = $1",
		where: "e.end_date < now()",
	},
}

// ListMySummaries は filter（プロフィール＋種別）に一致するイベントサマリーを取得する。
// filter.Kind・sort・order は呼び出し元（service 層）で検証済みであることを前提とする。
func (r *eventPostgres) ListMySummaries(ctx context.Context, filter model.MyEventFilter, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	clause, ok := myEventClauses[filter.Kind]
	if !ok {
		return nil, fmt.Errorf("list my event summaries: unknown kind %q", filter.Kind)
	}

	// G201: 埋め込むのは myEventClauses とホワイトリスト由来の固定文字列のみ。
	// profile_id / limit / offset は args 経由でのみ渡すため SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`%s
		%s
		WHERE %s
		ORDER BY %s
		LIMIT $2 OFFSET $3`, eventSummarySelect, clause.join, clause.where, orderByClause(sort, order))

	rows, err := r.db.QueryContext(ctx, query, filter.ProfileID.String(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list my event summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries, err := scanEventSummaries(rows)
	if err != nil {
		return nil, err
	}
	if err := r.attachTagsToSummaries(ctx, summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

// CountMyEventKinds は指定プロフィールの種別ごとの件数を1クエリ（種別ごとのスカラーサブクエリ）で返す。
func (r *eventPostgres) CountMyEventKinds(ctx context.Context, profileID uuid.UUID) (model.MyEventCounts, error) {
	countSubquery := func(kind model.MyEventKind) string {
		clause := myEventClauses[kind]
		return fmt.Sprintf("(SELECT COUNT(*) FROM events e %s WHERE %s)", clause.join, clause.where)
	}

	// G201: 埋め込むのは myEventClauses 由来の固定文字列のみ。
	// profile_id は args 経由でのみ渡すため SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`SELECT %s, %s, %s`,
		countSubquery(model.MyEventKindHosted),
		countSubquery(model.MyEventKindApplied),
		countSubquery(model.MyEventKindAttended),
	)

	var counts model.MyEventCounts
	if err := r.db.QueryRowContext(ctx, query, profileID.String()).Scan(
		&counts.Hosted,
		&counts.Applied,
		&counts.Attended,
	); err != nil {
		return model.MyEventCounts{}, fmt.Errorf("count my events: %w", err)
	}
	return counts, nil
}

// Create はイベントを関連テーブル（費用・持ち物・画像・PDF）とともに
// トランザクション内で一括登録する。
func (r *eventPostgres) Create(ctx context.Context, e *model.NewEvent) (model.CreateEventResponse, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CreateEventResponse{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// events テーブルへ INSERT し、生成 ID と作成日時を取得する。
	const insertEvent = `
		INSERT INTO events (id, profile_id, title, description, location, event_date, end_date, application_deadline, capacity, external_url)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at`

	var resp model.CreateEventResponse
	err = tx.QueryRowContext(ctx, insertEvent,
		e.ProfileID,
		e.Title,
		nullString(e.Description),
		nullString(e.Location),
		e.EventDate,
		e.EndDate,
		nullTime(e.ApplicationDeadline),
		nullInt32(e.Capacity),
		nullString(e.ExternalURL),
	).Scan(&resp.ID, &resp.CreatedAt)
	if err != nil {
		return model.CreateEventResponse{}, fmt.Errorf("insert event: %w", err)
	}

	// event_costs テーブルへ INSERT する。
	const insertCost = `
		INSERT INTO event_costs (id, event_id, category, cost)
		VALUES (gen_random_uuid(), $1, $2, $3)`

	for _, c := range e.Costs {
		if _, err := tx.ExecContext(ctx, insertCost, resp.ID, c.Category, c.Cost); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event cost: %w", err)
		}
	}

	// event_items テーブルへ INSERT する。
	const insertItem = `
		INSERT INTO event_items (id, event_id, event_item, is_required)
		VALUES (gen_random_uuid(), $1, $2, $3)`

	for _, item := range e.Items {
		if _, err := tx.ExecContext(ctx, insertItem, resp.ID, item.Item, item.IsRequired); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event item: %w", err)
		}
	}

	// event_images テーブルへ INSERT する。filename は同順の要素（範囲外は空文字）。
	const insertImage = `
		INSERT INTO event_images (id, event_id, image_objectkey, filename)
		VALUES (gen_random_uuid(), $1, $2, $3)`

	for i, key := range e.ImageObjectKeys {
		if _, err := tx.ExecContext(ctx, insertImage, resp.ID, key, filenameAt(e.ImageFilenames, i)); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event image: %w", err)
		}
	}

	// event_pdfs テーブルへ INSERT する。filename は同順の要素（範囲外は空文字）。
	const insertPDF = `
		INSERT INTO event_pdfs (id, event_id, pdf_objectkey, filename)
		VALUES (gen_random_uuid(), $1, $2, $3)`

	for i, key := range e.PdfObjectKeys {
		if _, err := tx.ExecContext(ctx, insertPDF, resp.ID, key, filenameAt(e.PdfFilenames, i)); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event pdf: %w", err)
		}
	}

	// event_tags テーブルへ INSERT する。
	// ON CONFLICT DO NOTHING は防御的措置。create パスでは event を直前に新規 INSERT しており、
	// TagIDs は service 層で正準形へ正規化・重複除去済みのため PK 衝突は通常起きないが、
	// 想定外の重複でトランザクション全体を巻き戻さないための保険として付ける。
	const insertTag = `
		INSERT INTO event_tags (event_id, tag_id)
		VALUES ($1, $2)
		ON CONFLICT (event_id, tag_id) DO NOTHING`

	for _, tagID := range e.TagIDs {
		if _, err := tx.ExecContext(ctx, insertTag, resp.ID, tagID); err != nil {
			// tag_id の FK 違反(23503)は存在しないタグID → ErrTagNotFound。
			// event_id は直前に INSERT 済みのため、ここで失敗し得る FK は tag_id 側のみ。
			// 将来 FK が増えても誤検知しないよう制約名(event_tags_tag_id_fkey)で識別する。
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "event_tags_tag_id_fkey" {
				return model.CreateEventResponse{}, fmt.Errorf("insert event tag %s: %w", tagID, ErrTagNotFound)
			}
			return model.CreateEventResponse{}, fmt.Errorf("insert event tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.CreateEventResponse{}, fmt.Errorf("commit transaction: %w", err)
	}

	return resp, nil
}

// GetOwnerProfileID は指定した eventID のイベント投稿者 profile_id を返す。
// profile_id は nullable のため sql.NullString で受け取る。
// 行が存在しない場合は repository.ErrEventNotFound を %w でラップして返す。
// 呼び出し側は errors.Is(err, repository.ErrEventNotFound) で判別できる。
func (r *eventPostgres) GetOwnerProfileID(ctx context.Context, eventID string) (string, error) {
	const query = `SELECT profile_id FROM events WHERE id = $1`

	var profileID sql.NullString
	if err := r.db.QueryRowContext(ctx, query, eventID).Scan(&profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		return "", fmt.Errorf("get event owner profile_id: %w", err)
	}
	return profileID.String, nil
}

// GetTitle は指定した eventID のイベントタイトルを返す。
// 行が存在しない場合は repository.ErrEventNotFound を %w でラップして返す。
// 呼び出し側は errors.Is(err, repository.ErrEventNotFound) で判別できる。
func (r *eventPostgres) GetTitle(ctx context.Context, eventID string) (string, error) {
	const query = `SELECT title FROM events WHERE id = $1`

	var title string
	if err := r.db.QueryRowContext(ctx, query, eventID).Scan(&title); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		return "", fmt.Errorf("get event title: %w", err)
	}
	return title, nil
}

// Exists は指定 eventID のイベントが存在するかを返す。
// eventID はパース済み uuid.UUID を受け取り、正規化文字列でクエリする
// （uuid.Parse は受理するが Postgres が拒否する形式を弾くため）。
func (r *eventPostgres) Exists(ctx context.Context, eventID uuid.UUID) (bool, error) {
	const query = `SELECT 1 FROM events WHERE id = $1`

	var one int
	if err := r.db.QueryRowContext(ctx, query, eventID.String()).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check event exists: %w", err)
	}
	return true, nil
}

// CancelWithNotification は指定したイベントを取りやめ状態にし、同一トランザクション内で
// 参加者への通知メールを event_notification_outbox に1件予約する。
//
// 非冪等: cancelled_at が NULL の行のみを UPDATE 対象にすることで、既にキャンセル済みの
// イベントに対する2回目以降の呼び出しを検出する。UPDATE が0行の場合、イベント自体の
// 存在有無を再確認し、存在しなければ ErrEventNotFound、存在すれば（＝既にキャンセル済み）
// ErrEventAlreadyCancelled を返す。
func (r *eventPostgres) CancelWithNotification(ctx context.Context, eventID uuid.UUID, subject, body string) (time.Time, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const updateQuery = `
		UPDATE events
		SET cancelled_at = now(),
		    updated_at = now()
		WHERE id = $1 AND cancelled_at IS NULL
		RETURNING cancelled_at`

	var cancelledAt time.Time
	err = tx.QueryRowContext(ctx, updateQuery, eventID.String()).Scan(&cancelledAt)
	if errors.Is(err, sql.ErrNoRows) {
		// UPDATE が0行 → イベントが存在しないか、既にキャンセル済みのいずれか。
		const selectQuery = `SELECT cancelled_at FROM events WHERE id = $1`
		var existingCancelledAt sql.NullTime
		selectErr := tx.QueryRowContext(ctx, selectQuery, eventID.String()).Scan(&existingCancelledAt)
		if errors.Is(selectErr, sql.ErrNoRows) {
			return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		if selectErr != nil {
			return time.Time{}, fmt.Errorf("check event cancelled: %w", selectErr)
		}
		return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventAlreadyCancelled)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("cancel event: %w", err)
	}

	const insertOutbox = `
		INSERT INTO event_notification_outbox (event_id, subject, body)
		VALUES ($1, $2, $3)`
	if _, err := tx.ExecContext(ctx, insertOutbox, eventID.String(), subject, body); err != nil {
		return time.Time{}, fmt.Errorf("insert notification outbox: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, fmt.Errorf("commit transaction: %w", err)
	}
	return cancelledAt, nil
}

func (r *eventPostgres) GetByID(ctx context.Context, id string) (*model.EventResponse, error) {
	// 参加人数は event_members.party_size の合計（参加キャンセル時は行ごと削除される）。
	// 集計方針は ADR-0024 を参照。
	const query = `
		SELECT		e.id, e.title, e.description, e.location, e.event_date, e.end_date, e.application_deadline,
					e.capacity, e.external_url, e.cancelled_at, e.created_at, e.updated_at,
					COALESCE((
						SELECT	SUM(m.party_size)
						FROM	event_members m
						WHERE	m.event_id = e.id
					), 0),
					p.id, p.display_name, p.avatar_url
		FROM 		events e
		LEFT JOIN  	profiles p ON p.id = e.profile_id
		WHERE 		e.id = $1`

	var (
		e model.EventResponse
		p model.ProfileSummary

		desc                sql.NullString
		location            sql.NullString
		externalURL         sql.NullString
		avatarURL           sql.NullString
		capacityNull        sql.NullInt32
		cancelledAt         sql.NullTime
		applicationDeadline sql.NullTime
		pID                 sql.NullString
		displayName         sql.NullString
	)

	// 初期化（JSON安定化）
	e.Costs = []model.EventCostResponse{}
	e.Items = []model.EventItemResponse{}
	e.ImageObjectKeys = []string{}
	e.PdfObjectKeys = []string{}
	e.ImageFilenames = []string{}
	e.PdfFilenames = []string{}
	e.Tags = []model.TagResponse{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&e.ID,
		&e.Title,
		&desc,
		&location,
		&e.EventDate,
		&e.EndDate,
		&applicationDeadline,
		&capacityNull,
		&externalURL,
		&cancelledAt,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.ParticipantCount,
		&pID,
		&displayName,
		&avatarURL,
	)

	if err != nil {
		return nil, fmt.Errorf("get event by id: %w", err)
	}

	// NULL安全変換
	if desc.Valid {
		e.Description = desc.String
	}
	if location.Valid {
		e.Location = location.String
	}
	if externalURL.Valid {
		e.ExternalURL = externalURL.String
	}
	if avatarURL.Valid {
		p.AvatarURL = avatarURL.String
	}
	if capacityNull.Valid {
		e.Capacity = int(capacityNull.Int32)
	}
	if cancelledAt.Valid {
		e.CancelledAt = &cancelledAt.Time
	}
	if applicationDeadline.Valid {
		e.ApplicationDeadline = &applicationDeadline.Time
	}
	if pID.Valid {
		p.ID = pID.String
	}
	if displayName.Valid {
		p.DisplayName = displayName.String
	}

	// profile構築
	e.Profile = p

	// costs
	const costQuery = `
		SELECT 	category, cost
		FROM 	event_costs
		WHERE 	event_id = $1`

	rows, err := r.db.QueryContext(ctx, costQuery, id)
	if err != nil {
		return nil, fmt.Errorf("get costs: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var c model.EventCostResponse
		if err := rows.Scan(&c.Category, &c.Cost); err != nil {
			return nil, fmt.Errorf("scan cost: %w", err)
		}
		e.Costs = append(e.Costs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate costs: %w", err)
	}

	// items
	const itemQuery = `
		SELECT 	event_item, is_required
		FROM 	event_items
		WHERE 	event_id = $1`

	itemRows, err := r.db.QueryContext(ctx, itemQuery, id)
	if err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}
	defer func() {
		_ = itemRows.Close()
	}()

	for itemRows.Next() {
		var i model.EventItemResponse
		if err := itemRows.Scan(&i.Item, &i.IsRequired); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		e.Items = append(e.Items, i)
	}
	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}

	// images（objectkey と filename を同順で取得する）
	const imageQuery = `
		SELECT 	image_objectkey, filename
		FROM 	event_images
		WHERE 	event_id = $1`

	imageRows, err := r.db.QueryContext(ctx, imageQuery, id)
	if err != nil {
		return nil, fmt.Errorf("get images: %w", err)
	}
	defer func() {
		_ = imageRows.Close()
	}()

	for imageRows.Next() {
		var key, filename string
		if err := imageRows.Scan(&key, &filename); err != nil {
			return nil, fmt.Errorf("scan image: %w", err)
		}
		e.ImageObjectKeys = append(e.ImageObjectKeys, key)
		e.ImageFilenames = append(e.ImageFilenames, filename)
	}
	if err := imageRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate images: %w", err)
	}

	// pdfs（objectkey と filename を同順で取得する）
	const pdfQuery = `
		SELECT 	pdf_objectkey, filename
		FROM 	event_pdfs
		WHERE 	event_id = $1`

	pdfRows, err := r.db.QueryContext(ctx, pdfQuery, id)
	if err != nil {
		return nil, fmt.Errorf("get pdfs: %w", err)
	}
	defer func() {
		_ = pdfRows.Close()
	}()

	for pdfRows.Next() {
		var key, filename string
		if err := pdfRows.Scan(&key, &filename); err != nil {
			return nil, fmt.Errorf("scan pdf: %w", err)
		}
		e.PdfObjectKeys = append(e.PdfObjectKeys, key)
		e.PdfFilenames = append(e.PdfFilenames, filename)
	}
	if err := pdfRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pdfs: %w", err)
	}

	// tags
	const tagQuery = `
		SELECT 	t.id, t.name
		FROM 	event_tags et
		JOIN 	tags t ON t.id = et.tag_id
		WHERE 	et.event_id = $1
		ORDER BY t.name ASC`

	tagRows, err := r.db.QueryContext(ctx, tagQuery, id)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer func() {
		_ = tagRows.Close()
	}()

	for tagRows.Next() {
		var tag model.TagResponse
		if err := tagRows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		e.Tags = append(e.Tags, tag)
	}
	if err := tagRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}

	return &e, nil
}
