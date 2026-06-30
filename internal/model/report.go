package model

import "time"

// CreateReportRequest はレポート投稿エンドポイントのリクエストボディ DTO。
//
// @Description	レポート投稿に必要な情報。
type CreateReportRequest struct {
	// EventID はレポート対象のイベントID（必須・255文字以内）。
	EventID string `json:"eventId" example:"event_1234567890" validate:"required,max=255"`
	// Content はレポート内容（必須・10,000文字以内）。
	Content string `json:"content" example:"多摩川アメリカザリガニ殲滅作戦の結果、参加者：5人、アメリカザリガニ防除数：138匹でした。" validate:"required,max=10000"`
	// ImageObjectKeys は画像オブジェクトキーの一覧（任意）。
	ImageObjectKeys []string `json:"imageObjectKeys,omitempty" validate:"omitempty,dive"`
	// PdfObjectKeys はPDFオブジェクトキーの一覧（任意・各要素255文字以内）。
	PdfObjectKeys []string `json:"pdfObjectKeys,omitempty" validate:"omitempty,dive,max=255"`
}

// NewReport は検証済みのレポートドメイン型。repository 層に渡す。
//
// 投稿者は events.profile_id で表現され（投稿者＝イベント投稿者を service 層で強制）、
// reports テーブルに profile_id を持たないためドメイン型にも保持しない。
type NewReport struct {
	EventID         string
	Content         string
	ImageObjectKeys []string
	PdfObjectKeys   []string
}

// CreateReportResponse はレポート投稿エンドポイントのレスポンス DTO。
type CreateReportResponse struct {
	// ReportID は作成されたレポートのID。
	ReportID string `json:"reportId" example:"report_1234567890"`
	// CreatedAt はレコード作成日時(RFC3339)。DB の TIMESTAMPTZ を UTC で保持する。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-26T03:08:24Z"`
}

// ReportResponse はレポート取得エンドポイントのレスポンス型。
//
// 1 イベント 1 レポート（reports.event_id は UNIQUE）のため、event_id 起点で取得する。
type ReportResponse struct {
	// ID はレポートの一意識別子(UUID)。
	ID string `json:"id" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// EventID は対象イベントのID(UUID)。
	EventID string `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// Content はレポート内容。
	Content string `json:"content" example:"多摩川アメリカザリガニ殲滅作戦の結果、参加者：5人、防除数：138匹でした。"`
	// ImageObjectKeys は画像オブジェクトキーの一覧。
	ImageObjectKeys []string `json:"imageObjectKeys"`
	// PdfObjectKeys は PDF オブジェクトキーの一覧。
	PdfObjectKeys []string `json:"pdfObjectKeys"`
	// ImageUrls は ImageObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	ImageUrls []string `json:"imageUrls"`
	// PdfUrls は PdfObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	PdfUrls []string `json:"pdfUrls"`
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-26T03:08:24Z"`
	// UpdatedAt はレコード更新日時(RFC3339)。
	UpdatedAt time.Time `json:"updatedAt" example:"2026-06-26T03:08:24Z"`
}
