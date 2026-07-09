package model

import (
	"time"

	"github.com/google/uuid"
)

// EventParticipationLog は event_participation_logs テーブルと対応するモデル。
// Repository 層で INSERT する際に使用する。追記のみで状態検証は行わない。
type EventParticipationLog struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	ProfileID uuid.UUID
	Action    string
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
