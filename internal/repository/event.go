package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
	// SearchSummaries は q に部分一致するイベントサマリーを指定ソート順で取得する。
	// title/description/location/主催者名(display_name)/持ち物(event_item) を横断検索する。
	SearchSummaries(ctx context.Context, q, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountSearchSummaries は q に部分一致するイベントの件数を返す。
	CountSearchSummaries(ctx context.Context, q string) (int, error)
	// GetByID は指定されたイベント ID の詳細情報を取得する。
	GetByID(ctx context.Context, id string) (*model.EventResponse, error)
	// Create はイベントを関連テーブルとともにトランザクション内で一括登録する。
	Create(ctx context.Context, e *model.NewEvent) (model.CreateEventResponse, error)
	// GetOwnerProfileID は指定した eventID のイベント投稿者 profile_id を返す。
	// イベントが存在しない場合は sql.ErrNoRows を %w でラップして返す。
	GetOwnerProfileID(ctx context.Context, eventID string) (string, error)
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
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.event_date ASC, e.id
		LIMIT $1 OFFSET $2`,
	"event_date:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.event_date DESC, e.id
		LIMIT $1 OFFSET $2`,
	"created_at:asc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		ORDER BY e.created_at ASC, e.id
		LIMIT $1 OFFSET $2`,
	"created_at:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
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
			&s.CreatedAt,
			&pID,
			&displayName,
			&avatarURL,
		); err != nil {
			return nil, fmt.Errorf("scan event summary: %w", err)
		}
		s.Location = location.String
		s.ProfileID = profileID.String
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

// searchWhereClause は SearchSummaries / CountSearchSummaries 共通の検索条件。
// title/description/主催者名(display_name)/location/持ち物(event_item) を横断検索する。
// $1 には "%" + escapeLike(q) + "%" を渡す。
const searchWhereClause = `
	WHERE e.title ILIKE $1
	   OR e.description ILIKE $1
	   OR p.display_name ILIKE $1
	   OR e.location ILIKE $1
	   OR EXISTS (SELECT 1 FROM event_items i WHERE i.event_id = e.id AND i.event_item ILIKE $1)`

// escapeLike は ILIKE のワイルドカード(% _)とエスケープ文字(\)を無効化し、
// ユーザー入力を純粋な部分一致文字列として扱う。PostgreSQL の ILIKE は
// デフォルトのエスケープ文字が \ のため ESCAPE 句は不要。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// searchSummariesQueries は (sort, order) の組み合わせから安全なクエリ文字列へのマップ。
// ユーザー入力を直接 SQL に埋め込まず、ホワイトリストから固定文字列を選ぶ。
// $1=検索パターン、$2=limit、$3=offset。
var searchSummariesQueries = map[string]string{
	"event_date:asc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		` + searchWhereClause + `
		ORDER BY e.event_date ASC, e.id
		LIMIT $2 OFFSET $3`,
	"event_date:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		` + searchWhereClause + `
		ORDER BY e.event_date DESC, e.id
		LIMIT $2 OFFSET $3`,
	"created_at:asc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		` + searchWhereClause + `
		ORDER BY e.created_at ASC, e.id
		LIMIT $2 OFFSET $3`,
	"created_at:desc": `
		SELECT e.id, e.title, e.event_date, e.location, e.profile_id, e.created_at,
		       p.id, p.display_name, p.avatar_url
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		` + searchWhereClause + `
		ORDER BY e.created_at DESC, e.id
		LIMIT $2 OFFSET $3`,
}

// SearchSummaries は q に部分一致するイベントサマリーを指定ソート順で取得する。
// title/description/location/主催者名(display_name)/持ち物(event_item) を横断検索する。
// sort・order は呼び出し元（service 層）でホワイトリスト検証済みであることを前提とする。
func (r *eventPostgres) SearchSummaries(ctx context.Context, q, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	key := sort + ":" + order
	query, ok := searchSummariesQueries[key]
	if !ok {
		// フォールバック: created_at DESC（service 層で正規化済みのため通常到達しない）。
		query = searchSummariesQueries["created_at:desc"]
	}

	pattern := "%" + escapeLike(q) + "%"

	rows, err := r.db.QueryContext(ctx, query, pattern, limit, offset)
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
			&s.CreatedAt,
			&pID,
			&displayName,
			&avatarURL,
		); err != nil {
			return nil, fmt.Errorf("scan event summary: %w", err)
		}
		s.Location = location.String
		s.ProfileID = profileID.String
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

// CountSearchSummaries は q に部分一致するイベントの件数を返す。
// LEFT JOIN profiles は 1 対 1、持ち物は EXISTS のため行の重複は起きず COUNT(*) で正しい。
func (r *eventPostgres) CountSearchSummaries(ctx context.Context, q string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM events e
		LEFT JOIN profiles p ON p.id = e.profile_id
		` + searchWhereClause

	pattern := "%" + escapeLike(q) + "%"

	var count int
	if err := r.db.QueryRowContext(ctx, query, pattern).Scan(&count); err != nil {
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
// 行が存在しない場合は sql.ErrNoRows を %w でラップして返す。
func (r *eventPostgres) GetOwnerProfileID(ctx context.Context, eventID string) (string, error) {
	const query = `SELECT profile_id FROM events WHERE id = $1`

	var profileID sql.NullString
	if err := r.db.QueryRowContext(ctx, query, eventID).Scan(&profileID); err != nil {
		return "", fmt.Errorf("get event owner profile_id: %w", err)
	}
	return profileID.String, nil
}

func (r *eventPostgres) GetByID(ctx context.Context, id string) (*model.EventResponse, error) {
	const query = `
		SELECT		e.id, e.title, e.description, e.location, e.event_date,
					e.capacity, e.external_url, e.created_at, e.updated_at,
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
