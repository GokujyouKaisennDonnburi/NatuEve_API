package model

import "time"

// EventCostInput はイベント費用の入力 DTO（カテゴリと金額）。
type EventCostInput struct {
	// Category は費用カテゴリ（例: "参加費"）。
	Category string `json:"category"`
	// Cost は費用（円）。0 以上の整数。
	Cost int `json:"cost"`
}

// EventItemInput はイベント持ち物の入力 DTO。
type EventItemInput struct {
	// Item は持ち物名（例: "双眼鏡"）。
	Item string `json:"item"`
	// IsRequired は必須かどうか。
	IsRequired bool `json:"isRequired"`
}

// CreateEventRequest はイベント投稿エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント投稿に必要な情報。
type CreateEventRequest struct {
	// Title はイベントタイトル（必須・255文字以内）。
	Title string `json:"title" example:"サクラ観察会"`
	// Description はイベント説明（必須）。
	Description string `json:"description" example:"春の桜を観察するイベントです。"`
	// Location は開催場所（必須・255文字以内）。
	Location string `json:"location" example:"東京都新宿御苑"`
	// EventDate はイベント開催日時(RFC3339)（必須）。
	EventDate time.Time `json:"eventDate" example:"2026-07-01T10:00:00Z"`
	// Capacity は定員（任意・0=未設定・正数=定員）。
	Capacity int `json:"capacity,omitempty" example:"30"`
	// ExternalURL は関連URLs（任意・255文字以内・http/https）。
	ExternalURL string `json:"externalUrl,omitempty" example:"https://example.com/event"`
	// Costs は費用内訳（1件以上必須）。
	Costs []EventCostInput `json:"costs"`
	// Items は持ち物リスト（任意）。
	Items []EventItemInput `json:"items,omitempty"`
	// ImageObjectKeys は画像オブジェクトキーの一覧（任意）。
	ImageObjectKeys []string `json:"imageObjectKeys,omitempty"`
	// PdfObjectKeys はPDFオブジェクトキーの一覧（任意・各要素255文字以内）。
	PdfObjectKeys []string `json:"pdfObjectKeys,omitempty"`
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
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-22T12:00:00Z"`
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
