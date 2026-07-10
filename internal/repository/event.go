package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// nullInt32 は 0 を NULL として扱う（未設定を表す）。
// capacity は定員数であり int32 の範囲内であることが仕様上保証されているため変換する。
func nullInt32(n int) sql.NullInt32 {
	if n == 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(n), Valid: true} //nolint:gosec
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
	// SearchSummaries は keywords すべてに一致するイベントサマリーを指定ソート順で取得する。
	// 各キーワードは title/description/location/主催者名(display_name)/持ち物(event_item)
	// を横断（OR）し、キーワード間は AND で結合する（AND 検索）。keywords は 1 件以上を前提とする。
	SearchSummaries(ctx context.Context, keywords []string, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountSearchSummaries は keywords すべてに一致するイベントの件数を返す。keywords は 1 件以上を前提とする。
	CountSearchSummaries(ctx context.Context, keywords []string) (int, error)
	// GetByID は指定されたイベント ID の詳細情報を取得する。
	GetByID(ctx context.Context, id string) (*model.EventResponse, error)
	// Create はイベントを関連テーブルとともにトランザクション内で一括登録する。
	Create(ctx context.Context, e *model.NewEvent) (model.CreateEventResponse, error)
	// GetOwnerProfileID は指定した eventID のイベント投稿者 profile_id を返す。
	// イベントが存在しない場合は sql.ErrNoRows を %w でラップして返す。
	GetOwnerProfileID(ctx context.Context, eventID string) (string, error)
	// Exists は指定した eventID のイベントが存在するかを返す。
	// 存在しない場合は (false, nil)、それ以外のエラーは %w でラップして返す。
	// eventID はパース済みの uuid.UUID を受け取り、正規化文字列でクエリする。
	Exists(ctx context.Context, eventID uuid.UUID) (bool, error)
	// Cancel は指定したイベントを取りやめ状態にする。
	// 既にキャンセル済みの場合も冪等に成功する。
	// イベントが存在しない場合は ErrEventNotFound を %w でラップして返す。
	Cancel(ctx context.Context, eventID uuid.UUID) (cancelledAt time.Time, err error)
}

// eventPostgres は EventRepository の PostgreSQL 実装。
type eventPostgres struct {
	db *sql.DB
}

// NewEventRepository は *sql.DB を使う EventRepository を生成する。
func NewEventRepository(db *sql.DB) EventRepository {
	return &eventPostgres{db: db}
}

// listSummariesQueries は (sort, order) の組み合わせから安全なクエリ文字列へのマップ。
// ユーザー入力を直接 SQL に埋め込まず、ホワイトリストから固定文字列を選ぶ。
var listSummariesQueries = map[string]string{
	"event_date:asc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.cancelled_at, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.event_date ASC, e.id
		LIMIT $1 OFFSET $2`,
	"event_date:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.cancelled_at, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.event_date DESC, e.id
		LIMIT $1 OFFSET $2`,
	"created_at:asc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.cancelled_at, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.created_at ASC, e.id
		LIMIT $1 OFFSET $2`,
	"created_at:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.cancelled_at, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.created_at DESC, e.id
		LIMIT $1 OFFSET $2`,
}

// ListSummaries は一覧表示に必要なカラムのみ SELECT する。
// description / external_url / capacity / updated_at は取得しない。
// sort・order は呼び出し元（service 層）でホワイトリスト検証済みであることを前提とする。
func (r *eventPostgres) ListSummaries(ctx context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	key := sort + ":" + order
	query, ok := listSummariesQueries[key]
	if !ok {
		// フォールバック: created_at DESC（service 層で正規化済みのため通常到達しない）。
		query = listSummariesQueries["created_at:desc"]
	}

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list event summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []model.EventSummary
	for rows.Next() {
		var s model.EventSummary
		var (
			location    sql.NullString
			profileID   sql.NullString
			cancelledAt sql.NullTime
			pID         sql.NullString
			displayName sql.NullString
			avatarURL   sql.NullString
		)
		if err := rows.Scan(
			&s.ID,
			&s.Title,
			&s.EventDate,
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

	// レコードが 0 件でも nil ではなく空スライスを返す。
	if summaries == nil {
		summaries = []model.EventSummary{}
	}
	return summaries, nil
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

// searchOrderByClauses は (sort, order) の組み合わせから安全な ORDER BY 句へのマップ。
// ユーザー入力を直接 SQL に埋め込まず、ホワイトリストから固定文字列を選ぶ。
// 同一ソートキーは id 昇順で安定ソートする。
var searchOrderByClauses = map[string]string{
	"event_date:asc":  "e.event_date ASC, e.id",
	"event_date:desc": "e.event_date DESC, e.id",
	"created_at:asc":  "e.created_at ASC, e.id",
	"created_at:desc": "e.created_at DESC, e.id",
}

// normalizeSearchText は照合基準を全角/半角で揃えるため NFKC 正規化する。
// 全角数字→半角数字、全角英字→半角英字、半角カナ→全角カナ 等を吸収する
// （ひらがな↔カタカナは対象外）。SQL 側の normalize(col, NFKC) と同一の正規化形を用いることで、
// 保存値とキーワードの表記ゆれ（半角/全角）を一致させる。
func normalizeSearchText(s string) string {
	return norm.NFKC.String(s)
}

// buildSearchWhere は keywords を AND 検索する WHERE 句本体と ILIKE パターン引数を返す。
// 各キーワードは5フィールド(title/description/display_name/location/持ち物)を OR で横断する
// 1グループとなり、グループ間は AND で連結する。プレースホルダは $startIdx から連番で割り当てる。
// キーワードは常にプレースホルダ経由で渡し、SQL 文字列へ直接埋め込まない（SQLインジェクション対策）。
// 半角/全角を同一視するため、カラム側は normalize(col, NFKC)、キーワード側は
// normalizeSearchText で NFKC 正規化する（両辺を同じ正規化形にそろえる）。
// keywords は 1 件以上であることを前提とする（0 件だと空の WHERE となり不正な SQL になる）。
func buildSearchWhere(keywords []string, startIdx int) (string, []any) {
	groups := make([]string, len(keywords))
	args := make([]any, len(keywords))
	for i, kw := range keywords {
		ph := fmt.Sprintf("$%d", startIdx+i)
		// %[1]s は同一プレースホルダを5箇所へ展開する（ワイヤプロトコル上、同一 $N の複数参照は正当）。
		groups[i] = fmt.Sprintf(
			"(normalize(e.title, NFKC) ILIKE %[1]s OR normalize(e.description, NFKC) ILIKE %[1]s "+
				"OR normalize(p.display_name, NFKC) ILIKE %[1]s OR normalize(e.location, NFKC) ILIKE %[1]s "+
				"OR EXISTS (SELECT 1 FROM event_items it WHERE it.event_id = e.id "+
				"AND normalize(it.event_item, NFKC) ILIKE %[1]s))",
			ph,
		)
		// NFKC 正規化 → LIKE エスケープ → % で囲む の順。全角％(U+FF05)は NFKC で ASCII '%' に
		// なるため、正規化を先に行い escapeLike でワイルドカードとして無効化する必要がある。
		args[i] = "%" + escapeLike(normalizeSearchText(kw)) + "%"
	}
	return strings.Join(groups, " AND "), args
}

// SearchSummaries は keywords すべてに一致するイベントサマリーを指定ソート順で取得する（AND 検索）。
// 各キーワードは title/description/location/主催者名(display_name)/持ち物(event_item) を横断（OR）する。
// sort・order は呼び出し元（service 層）でホワイトリスト検証済みであることを前提とする。
func (r *eventPostgres) SearchSummaries(ctx context.Context, keywords []string, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	where, args := buildSearchWhere(keywords, 1)

	orderBy, ok := searchOrderByClauses[sort+":"+order]
	if !ok {
		// フォールバック: created_at DESC（service 層で正規化済みのため通常到達しない）。
		orderBy = searchOrderByClauses["created_at:desc"]
	}

	// limit / offset はキーワード分のプレースホルダの後ろに割り当てる。
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	// G201: 埋め込むのは buildSearchWhere が生成する列名+プレースホルダ番号($N)、
	// ホワイトリスト由来の ORDER BY、int のインデックスのみ。キーワード等のユーザー入力は
	// 一切文字列連結せず args 経由でのみ渡すため SQL インジェクションは発生しない。
	//nolint:gosec // 上記の理由により安全（ユーザー入力は文字列連結しない）
	query := fmt.Sprintf(`
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.cancelled_at, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, where, orderBy, limitIdx, offsetIdx)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search event summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []model.EventSummary
	for rows.Next() {
		var s model.EventSummary
		var (
			location    sql.NullString
			profileID   sql.NullString
			cancelledAt sql.NullTime
			pID         sql.NullString
			displayName sql.NullString
			avatarURL   sql.NullString
		)
		if err := rows.Scan(
			&s.ID,
			&s.Title,
			&s.EventDate,
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

	// レコードが 0 件でも nil ではなく空スライスを返す。
	if summaries == nil {
		summaries = []model.EventSummary{}
	}
	return summaries, nil
}

// CountSearchSummaries は keywords すべてに一致するイベントの件数を返す（AND 検索）。
// LEFT JOIN profiles は 1 対 1、持ち物は EXISTS のため行の重複は起きず COUNT(*) で正しい。
func (r *eventPostgres) CountSearchSummaries(ctx context.Context, keywords []string) (int, error) {
	where, args := buildSearchWhere(keywords, 1)

	// G201: 埋め込むのは buildSearchWhere が生成する列名+プレースホルダ番号($N)のみ。
	// キーワードは args 経由でのみ渡すため SQL インジェクションは発生しない。
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
		INSERT INTO events (id, profile_id, title, description, location, event_date, capacity, external_url)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	var resp model.CreateEventResponse
	err = tx.QueryRowContext(ctx, insertEvent,
		e.ProfileID,
		e.Title,
		nullString(e.Description),
		nullString(e.Location),
		e.EventDate,
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

// Cancel は指定したイベントを取りやめ状態にする。
// 既にキャンセル済みの場合も冪等に成功する。
// イベントが存在しない場合は ErrEventNotFound を %w でラップして返す。
func (r *eventPostgres) Cancel(ctx context.Context, eventID uuid.UUID) (time.Time, error) {
	const query = `
		UPDATE events
		SET cancelled_at = COALESCE(cancelled_at, now()),
		    updated_at = now()
		WHERE id = $1
		RETURNING cancelled_at`

	var cancelledAt time.Time
	if err := r.db.QueryRowContext(ctx, query, eventID.String()).Scan(&cancelledAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, fmt.Errorf("event %s: %w", eventID, ErrEventNotFound)
		}
		return time.Time{}, fmt.Errorf("cancel event: %w", err)
	}
	return cancelledAt, nil
}

func (r *eventPostgres) GetByID(ctx context.Context, id string) (*model.EventResponse, error) {
	const query = `
		SELECT		e.id, e.title, e.description, e.location, e.event_date,
					e.capacity, e.external_url, e.cancelled_at, e.created_at, e.updated_at,
					p.id, p.display_name, p.avatar_url
		FROM 		events e
		LEFT JOIN  	profiles p ON p.id = e.profile_id
		WHERE 		e.id = $1`

	var (
		e model.EventResponse
		p model.ProfileSummary

		desc         sql.NullString
		location     sql.NullString
		externalURL  sql.NullString
		avatarURL    sql.NullString
		capacityNull sql.NullInt32
		cancelledAt  sql.NullTime
		pID          sql.NullString
		displayName  sql.NullString
	)

	// 初期化（JSON安定化）
	e.Costs = []model.EventCostResponse{}
	e.Items = []model.EventItemResponse{}
	e.ImageObjectKeys = []string{}
	e.PdfObjectKeys = []string{}
	e.ImageFilenames = []string{}
	e.PdfFilenames = []string{}

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&e.ID,
		&e.Title,
		&desc,
		&location,
		&e.EventDate,
		&capacityNull,
		&externalURL,
		&cancelledAt,
		&e.CreatedAt,
		&e.UpdatedAt,
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

	return &e, nil
}
