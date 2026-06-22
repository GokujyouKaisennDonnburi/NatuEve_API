package model

import "time"

// Profile はアプリ側のユーザープロフィール(ドメイン型)。
//
// ID は Supabase Auth の JWT sub(UUID) と一致する。
type Profile struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProfileResponse はプロフィール取得のレスポンス DTO。
type ProfileResponse struct {
	// ID は Supabase Auth のユーザー ID(UUID)。
	ID string `json:"id" example:"d290f1ee-6c54-4b01-90e6-d701748f0851"`
	// Email はユーザーのメールアドレス。
	Email string `json:"email" example:"user@example.com"`
	// DisplayName は表示名(未設定なら空)。
	DisplayName string `json:"displayName" example:"なちゅいべ太郎"`
	// AvatarURL はアバター画像 URL(未設定なら空)。
	AvatarURL string `json:"avatarUrl" example:"https://example.com/avatar.png"`
	// CreatedAt はプロフィール作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-22T12:00:00Z"`
	// UpdatedAt はプロフィール更新日時(RFC3339)。
	UpdatedAt time.Time `json:"updatedAt" example:"2026-06-22T12:00:00Z"`
}

// NewProfileResponse は Profile から ProfileResponse を組み立てる。
func NewProfileResponse(p *Profile) ProfileResponse {
	return ProfileResponse{
		ID:          p.ID,
		Email:       p.Email,
		DisplayName: p.DisplayName,
		AvatarURL:   p.AvatarURL,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}
