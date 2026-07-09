package model

import (
	"github.com/google/uuid"
)

// Tag は tags テーブルに対応するモデル。
type Tag struct {
	ID             uuid.UUID
	Name           string
	NormalizedName string
}

// TagListResponse はタグ一覧のレスポンス DTO。
type TagListResponse struct {
	Tags []TagResponse `json:"tags"`
}

// TagResponse はタグ単体レスポンス DTO。
type TagResponse struct {
	// ID はタグの一意識別子(UUID)。
	ID string `json:"id" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// Name はタグ名。
	Name string `json:"name" example:"外来生物"`
	// NormalizedName は重複確認用タグ名。
	NormalizedName string `json:"normalized_name" example:"外来生物"`
}

// CreateTagRequest はタグ作成リクエスト DTO。
type CreateTagRequest struct {
	// Name はタグ名。
	Name string `json:"name" binding:"required,max=30" example:"外来生物"`
}
