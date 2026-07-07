package model

import (
	"time"

	"github.com/google/uuid"
)

// JoinEventRequest はイベント参加申込エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント参加申込に必要な情報。認証は任意。
type JoinEventRequest struct {
	// Username は参加するユーザーの表示名（必須・255文字以内）。
	Username string `json:"username" example:"山田太郎" validate:"required,max=255"`
	// MailAddress は参加するユーザーのメールアドレス（必須）。
	MailAddress string `json:"mailAddress" example:"yamada@example.com" validate:"required,email,max=255"`
	// PartySize は代表者を含む参加人数（必須・1以上）。
	PartySize int `json:"partySize" example:"1" validate:"required,min=1"`
}

// JoinEventResponse は参加申込完了時に返すレスポンス。
type JoinEventResponse struct {
	// EventID は参加したイベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID は参加するユーザーのUUID。ログイン時のみ記録され、匿名参加時は null。
	ProfileID *uuid.UUID `json:"profileId" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// Username は参加するユーザーの表示名。
	Username string `json:"username" example:"山田太郎"`
	// MailAddress は参加するユーザーのメールアドレス。
	MailAddress string `json:"mailAddress" example:"yamada@example.com"`
	// PartySize は代表者を含む参加人数。
	PartySize int `json:"partySize" example:"1"`
	// CreatedAt は参加申込日時。
	CreatedAt time.Time `json:"createdAt" example:"2023-01-01T12:00:00Z"`
}

// EventRecipient はイベント参加者への一斉送信の宛先1件分を表すモデル。
// Repository 層で event_members から SELECT する際に使用する。
type EventRecipient struct {
	MailAddress string
}

// EventMember は event_members テーブルと対応するモデル。
// Repository 層で INSERT・SELECT する際に使用する。
type EventMember struct {
	EventID     uuid.UUID
	ProfileID   uuid.NullUUID // ログイン時のみ Valid=true。匿名参加は Valid=false（DB上はNULL）。
	Username    string
	MailAddress string
	// PartySize は代表者を含む参加人数（1以上）。
	PartySize int
	CreatedAt time.Time
}
