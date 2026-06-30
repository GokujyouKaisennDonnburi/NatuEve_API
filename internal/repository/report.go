package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ReportRepository は reports テーブルへのアクセスを抽象化する。
type ReportRepository interface {
	// Create はレポートを関連テーブルとともにトランザクション内で一括登録する。
	Create(ctx context.Context, r *model.NewReport) (model.CreateReportResponse, error)
	// GetByEventID は指定したイベント ID に紐づくレポート詳細を取得する。
	// 1 イベント 1 レポート（reports.event_id は UNIQUE）を前提とする。
	// レポートが存在しない場合は sql.ErrNoRows を %w でラップして返す。
	GetByEventID(ctx context.Context, eventID string) (*model.ReportResponse, error)
}

// reportPostgres は ReportRepository の PostgreSQL 実装。
type reportPostgres struct {
	db *sql.DB
}

// NewReportRepository は *sql.DB を使う ReportRepository を生成する。
func NewReportRepository(db *sql.DB) ReportRepository {
	return &reportPostgres{db: db}
}

// Create はレポート関連テーブル（画像・PDF）を含めてトランザクション内で一括登録する。
func (r *reportPostgres) Create(ctx context.Context, report *model.NewReport) (model.CreateReportResponse, error) {
	// トランザクション開始
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CreateReportResponse{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// レポート本体を登録
	const insertReport = `
		INSERT INTO reports (id, event_id, content)
		VALUES (gen_random_uuid(), $1, $2)
		RETURNING id, created_at
	`
	// レポートIDと作成日時を取得
	var resp model.CreateReportResponse
	if err := tx.QueryRowContext(ctx, insertReport,
		report.EventID,
		report.Content,
	).Scan(&resp.ReportID, &resp.CreatedAt); err != nil {
		return model.CreateReportResponse{}, fmt.Errorf("insert report: %w", err)
	}

	// 画像・PDFの関連テーブルに登録
	if len(report.ImageObjectKeys) > 0 {
		const insertImage = `
			INSERT INTO report_images (id, report_id, image_objectkey)
			VALUES (gen_random_uuid(), $1, $2)
		`
		for _, objectKey := range report.ImageObjectKeys {
			if _, err := tx.ExecContext(ctx, insertImage, resp.ReportID, objectKey); err != nil {
				return model.CreateReportResponse{}, fmt.Errorf("insert report image: %w", err)
			}
		}
	}

	// PDFの関連テーブルに登録
	if len(report.PdfObjectKeys) > 0 {
		const insertPDF = `
			INSERT INTO report_pdfs (id, report_id, pdf_objectkey)
			VALUES (gen_random_uuid(), $1, $2)
		`
		for _, objectKey := range report.PdfObjectKeys {
			if _, err := tx.ExecContext(ctx, insertPDF, resp.ReportID, objectKey); err != nil {
				return model.CreateReportResponse{}, fmt.Errorf("insert report pdf: %w", err)
			}
		}
	}

	// トランザクションをコミット
	if err := tx.Commit(); err != nil {
		return model.CreateReportResponse{}, fmt.Errorf("commit transaction: %w", err)
	}

	return resp, nil
}

// GetByEventID は event_id 起点でレポート本体と関連テーブル（画像・PDF）を取得する。
// reports.event_id は UNIQUE のため、1 イベントにつき高々 1 件のレポートが返る。
func (r *reportPostgres) GetByEventID(ctx context.Context, eventID string) (*model.ReportResponse, error) {
	const query = `
		SELECT	id, event_id, content, created_at, updated_at
		FROM	reports
		WHERE	event_id = $1`

	var rep model.ReportResponse

	// 初期化（JSON 安定化）。0 件でも null ではなく [] を返す。
	rep.ImageObjectKeys = []string{}
	rep.PdfObjectKeys = []string{}

	if err := r.db.QueryRowContext(ctx, query, eventID).Scan(
		&rep.ID,
		&rep.EventID,
		&rep.Content,
		&rep.CreatedAt,
		&rep.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("get report by event id: %w", err)
	}

	// images
	const imageQuery = `
		SELECT	image_objectkey
		FROM	report_images
		WHERE	report_id = $1`

	imageRows, err := r.db.QueryContext(ctx, imageQuery, rep.ID)
	if err != nil {
		return nil, fmt.Errorf("get report images: %w", err)
	}
	defer func() { _ = imageRows.Close() }()

	for imageRows.Next() {
		var key string
		if err := imageRows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan report image: %w", err)
		}
		rep.ImageObjectKeys = append(rep.ImageObjectKeys, key)
	}
	if err := imageRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report images: %w", err)
	}

	// pdfs
	const pdfQuery = `
		SELECT	pdf_objectkey
		FROM	report_pdfs
		WHERE	report_id = $1`

	pdfRows, err := r.db.QueryContext(ctx, pdfQuery, rep.ID)
	if err != nil {
		return nil, fmt.Errorf("get report pdfs: %w", err)
	}
	defer func() { _ = pdfRows.Close() }()

	for pdfRows.Next() {
		var key string
		if err := pdfRows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan report pdf: %w", err)
		}
		rep.PdfObjectKeys = append(rep.PdfObjectKeys, key)
	}
	if err := pdfRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report pdfs: %w", err)
	}

	return &rep, nil
}
