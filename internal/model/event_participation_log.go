package model

import (
	"time"

	"github.com/google/uuid"
)

// EventParticipationLog は event_participation_logs テーブルと対応するモデル。
// Repository 層で SELECT する際に使用する。
type EventParticipationLog struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	ProfileID uuid.UUID
	Action    string // "join" または "leave"
	CreatedAt time.Time
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
