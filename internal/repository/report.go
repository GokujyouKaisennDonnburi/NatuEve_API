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
