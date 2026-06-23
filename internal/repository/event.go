package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/model"
)

// nullInt32 は 0 を NULL として扱う（未設定を表す）。
// capacity は定員数であり int32 の範囲内であることが仕様上保証されているため変換する。
func nullInt32(n int) sql.NullInt32 {
	if n == 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(n), Valid: true} //nolint:gosec
}

// EventRepository は events テーブルへのアクセスを抽象化する。
type EventRepository interface {
	// ListSummaries は指定されたソート順でイベントサマリーを取得する。
	// sort は "created_at" または "event_date"、order は "asc" または "desc"。
	// 同一ソートキーのレコードは id 昇順で安定ソートする。
	ListSummaries(ctx context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error)
	// CountSummaries は events テーブルの全件数を返す。
	CountSummaries(ctx context.Context) (int, error)
	// Create はイベントを関連テーブルとともにトランザクション内で一括登録する。
	Create(ctx context.Context, e *model.NewEvent) (model.CreateEventResponse, error)
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
		SELECT id, title, event_date, location, profile_id, created_at
		FROM events
		ORDER BY event_date ASC, id
		LIMIT $1 OFFSET $2`,
	"event_date:desc": `
		SELECT id, title, event_date, location, profile_id, created_at
		FROM events
		ORDER BY event_date DESC, id
		LIMIT $1 OFFSET $2`,
	"created_at:asc": `
		SELECT id, title, event_date, location, profile_id, created_at
		FROM events
		ORDER BY created_at ASC, id
		LIMIT $1 OFFSET $2`,
	"created_at:desc": `
		SELECT id, title, event_date, location, profile_id, created_at
		FROM events
		ORDER BY created_at DESC, id
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
			location  sql.NullString
			profileID sql.NullString
		)
		if err := rows.Scan(
			&s.ID,
			&s.Title,
			&s.EventDate,
			&location,
			&profileID,
			&s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event summary: %w", err)
		}
		s.Location = location.String
		s.ProfileID = profileID.String
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

	// event_images テーブルへ INSERT する。
	const insertImage = `
		INSERT INTO event_images (id, event_id, image_objectkey)
		VALUES (gen_random_uuid(), $1, $2)`

	for _, key := range e.ImageObjectKeys {
		if _, err := tx.ExecContext(ctx, insertImage, resp.ID, key); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event image: %w", err)
		}
	}

	// event_pdfs テーブルへ INSERT する。
	const insertPDF = `
		INSERT INTO event_pdfs (id, event_id, pdf_objectkey)
		VALUES (gen_random_uuid(), $1, $2)`

	for _, key := range e.PdfObjectKeys {
		if _, err := tx.ExecContext(ctx, insertPDF, resp.ID, key); err != nil {
			return model.CreateEventResponse{}, fmt.Errorf("insert event pdf: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.CreateEventResponse{}, fmt.Errorf("commit transaction: %w", err)
	}

	return resp, nil
}
