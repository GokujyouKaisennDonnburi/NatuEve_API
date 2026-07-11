package model

import "time"

// EventCostInput はイベント費用の入力 DTO（カテゴリと金額）。
type EventCostInput struct {
	// Category は費用カテゴリ（例: "参加費"）。
	Category string `json:"category" validate:"required,max=255"`
	// Cost は費用（円）。0 以上の整数。
	Cost int `json:"cost" validate:"min=0"`
}

// EventItemInput はイベント持ち物の入力 DTO。
type EventItemInput struct {
	// Item は持ち物名（例: "双眼鏡"）。
	Item string `json:"item" validate:"required,max=255"`
	// IsRequired は必須かどうか。
	IsRequired bool `json:"isRequired"`
}

// CreateEventRequest はイベント投稿エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント投稿に必要な情報。
type CreateEventRequest struct {
	// Title はイベントタイトル（必須・255文字以内）。
	Title string `json:"title" example:"サクラ観察会" validate:"required,max=255"`
	// Description はイベント説明（必須・10,000文字以内）。
	Description string `json:"description" example:"春の桜を観察するイベントです。" validate:"required,max=10000"`
	// Location は開催場所（必須・255文字以内）。
	Location string `json:"location" example:"東京都新宿御苑" validate:"required,max=255"`
	// EventDate はイベント開催日時(RFC3339)（必須）。
	EventDate time.Time `json:"eventDate" example:"2026-07-01T10:00:00Z" validate:"required"`
	// Capacity は定員（任意・0=未設定・正数=定員）。
	Capacity int `json:"capacity,omitempty" example:"30" validate:"min=0"`
	// ExternalURL は関連URLs（任意・255文字以内・http/https）。
	ExternalURL string `json:"externalUrl,omitempty" example:"https://example.com/event" validate:"omitempty,max=255"`
	// Costs は費用内訳（1件以上必須）。
	Costs []EventCostInput `json:"costs" validate:"required,min=1,dive"`
	// Items は持ち物リスト（任意）。
	Items []EventItemInput `json:"items,omitempty" validate:"omitempty,dive"`
	// ImageObjectKeys は画像オブジェクトキーの一覧（任意）。
	ImageObjectKeys []string `json:"imageObjectKeys,omitempty" validate:"omitempty,dive"`
	// PdfObjectKeys はPDFオブジェクトキーの一覧（任意・各要素255文字以内）。
	PdfObjectKeys []string `json:"pdfObjectKeys,omitempty" validate:"omitempty,dive,max=255"`
	// ImageFilenames は画像の元ファイル名一覧（任意）。指定時は ImageObjectKeys と同数・同順。
	// ダウンロード時のファイル名（Content-Disposition）と UI 表示に使う。
	ImageFilenames []string `json:"imageFilenames,omitempty" validate:"omitempty,dive,max=255"`
	// PdfFilenames はPDFの元ファイル名一覧（任意）。指定時は PdfObjectKeys と同数・同順。
	PdfFilenames []string `json:"pdfFilenames,omitempty" validate:"omitempty,dive,max=255"`
	// TagIDs は紐づけるタグの UUID 一覧（任意）。
	TagIDs []string `json:"tagIds,omitempty" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890" validate:"omitempty,dive"`
}

// NewEvent は検証済みのイベントドメイン型。repository 層に渡す。
type NewEvent struct {
	ProfileID       string
	Title           string
	Description     string
	Location        string
	EventDate       time.Time
	Capacity        int
	ExternalURL     string
	Costs           []EventCostInput
	Items           []EventItemInput
	ImageObjectKeys []string
	PdfObjectKeys   []string
	// ImageFilenames は ImageObjectKeys と同順の元ファイル名（未指定は空文字）。
	ImageFilenames []string
	// PdfFilenames は PdfObjectKeys と同順の元ファイル名（未指定は空文字）。
	PdfFilenames []string
	// TagIDs は紐づけるタグの UUID 一覧（trim・重複除去済み）。
	TagIDs []string
}

// CreateEventResponse はイベント投稿エンドポイントのレスポンス DTO。
type CreateEventResponse struct {
	// ID は生成されたイベントの UUID。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-23T12:00:00Z"`
}

// EventSummary はイベント一覧で返す DTO（詳細フィールドは含まない）。
type EventSummary struct {
	// ID はイベントの一意識別子(UUID)。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID は投稿者のプロフィール ID(UUID)。
	ProfileID string `json:"profileId" example:"d290f1ee-6c54-4b01-90e6-d701748f0851"`
	// Title はイベントタイトル。
	Title string `json:"title" example:"サクラ観察会"`
	// Location は開催場所（文字列）。
	Location string `json:"location" example:"東京都新宿御苑"`
	// EventDate はイベント開催日時(RFC3339)。
	EventDate time.Time `json:"eventDate" example:"2026-07-01T10:00:00Z"`
	// CancelledAt はイベントが取りやめになった日時(RFC3339)。nil の場合は開催予定。
	CancelledAt *time.Time `json:"cancelledAt,omitempty" example:"2026-06-25T10:00:00Z"`
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-22T12:00:00Z"`
	// ProfileSummary は投稿者プロフィールのサマリー情報。
	Profile ProfileSummary `json:"profile"`
	// Tags は紐づくタグの一覧（name 昇順）。タグが無い場合は省略される。
	Tags []TagResponse `json:"tags,omitempty"`
}

// EventListResponse はイベント一覧取得エンドポイントのレスポンス型。
//
// swag 用注釈のためにラッパー型を定義する。
type EventListResponse struct {
	// Events はイベントサマリーの一覧。
	Events []EventSummary `json:"events"`
	// TotalCount はフィルタなし全件数。クライアントが最終ページ offset を算出するために使う。
	TotalCount int `json:"totalCount" example:"153"`
	// Limit は正規化後の実際に使われた取得件数。
	Limit int `json:"limit" example:"20"`
	// Offset は正規化後の実際に使われた取得開始位置。
	Offset int `json:"offset" example:"0"`
}

// EventResponse はイベント詳細取得エンドポイントのレスポンス型。
type EventResponse struct {
	ID              string              `json:"id"`
	Profile         ProfileSummary      `json:"profile"`
	Title           string              `json:"title"`
	Description     string              `json:"description"`
	Location        string              `json:"location"`
	EventDate       time.Time           `json:"eventDate"`
	Capacity        int                 `json:"capacity"`
	ExternalURL     string              `json:"externalUrl"`
	Costs           []EventCostResponse `json:"costs"`
	Items           []EventItemResponse `json:"items"`
	ImageObjectKeys []string            `json:"imageObjectKeys"`
	PdfObjectKeys   []string            `json:"pdfObjectKeys"`
	// ImageFilenames は ImageObjectKeys に対応する元ファイル名（未設定は空文字）。
	ImageFilenames []string `json:"imageFilenames"`
	// PdfFilenames は PdfObjectKeys に対応する元ファイル名（未設定は空文字）。
	PdfFilenames []string `json:"pdfFilenames"`
	// ImageUrls は ImageObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	ImageUrls []string `json:"imageUrls"`
	// PdfUrls は PdfObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	PdfUrls []string `json:"pdfUrls"`
	// Tags は紐づくタグの一覧（name 昇順）。
	Tags      []TagResponse `json:"tags"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	// CancelledAt はイベントが取りやめになった日時(RFC3339)。nil の場合は開催予定。
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
}

// EventCostResponse はイベント費用のレスポンス DTO。
type EventCostResponse struct {
	Category string `json:"category"`
	Cost     int    `json:"cost"`
}

// EventItemResponse はイベント持ち物のレスポンス DTO。
type EventItemResponse struct {
	Item       string `json:"item"`
	IsRequired bool   `json:"isRequired"`
}

// CancelEventResponse はイベントキャンセルエンドポイントのレスポンス DTO。
type CancelEventResponse struct {
	// ID はキャンセルしたイベントの UUID。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// CancelledAt はキャンセル日時(RFC3339)。
	CancelledAt time.Time `json:"cancelledAt" example:"2026-06-25T10:00:00Z"`
}
