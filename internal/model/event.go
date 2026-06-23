package model

import "time"

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
