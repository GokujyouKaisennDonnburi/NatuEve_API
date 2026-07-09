package model

import (
	"time"

	"github.com/google/uuid"
)

// EventParticipationLog は event_participation_logs テーブルと対応するモデル。
// Repository 層で SELECT/INSERT する際に使用する。
type EventParticipationLog struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	ProfileID uuid.UUID
	Action    string // "join" または "leave"
	CreatedAt time.Time
}

// CreateParticipationLogRequest はイベント参加状態ログ追加エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント参加状態ログ追加に必要な情報。認証必須。
type CreateParticipationLogRequest struct {
	// Action は参加状態（必須・join または leave）。
	Action string `json:"action" example:"join" validate:"required,oneof=join leave"`
}

// ParticipationLogResponse はイベント参加状態ログ追加エンドポイントのレスポンス。
type ParticipationLogResponse struct {
	// ID は作成された参加状態ログのUUID。
	ID uuid.UUID `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// EventID は対象イベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// ProfileID は記録したユーザーのUUID。
	ProfileID uuid.UUID `json:"profileId" example:"c3d4e5f6-a7b8-9012-cdef-345678901234"`
	// Action は記録した参加状態。
	Action string `json:"action" example:"join"`
	// CreatedAt は記録日時。
	CreatedAt time.Time `json:"createdAt" example:"2026-07-01T12:00:00Z"`
}

// ParticipationStatusResponse はログイン中ユーザーのイベント参加状態取得エンドポイントのレスポンス DTO。
//
//	@Description	認証ユーザー自身の最新の参加状態。履歴が1件もない場合は action=null, participating=false, updatedAt=null。
type ParticipationStatusResponse struct {
	// Action は最新の参加アクション。履歴なしの場合は null。
	// "join" または "leave" のいずれか。
	Action *string `json:"action" example:"join" extensions:"x-nullable"`
	// Participating は最新アクションから派生した参加フラグ。
	// action="join" なら true、それ以外（leave or 履歴なし）は false。
	Participating bool `json:"participating" example:"true"`
	// UpdatedAt は最新アクションの発生日時(RFC3339)。履歴なしの場合は null。
	UpdatedAt *time.Time `json:"updatedAt" example:"2026-07-01T12:00:00Z" extensions:"x-nullable"`
}
