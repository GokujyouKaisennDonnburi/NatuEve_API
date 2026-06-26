package service

import (
	"context"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// AuthenticatedUser は認証済みユーザーの入力 DTO。
//
// middleware への逆依存を避けるため service 側に定義する。
// handler が middleware の AuthUser からこの型へ詰め替える。
type AuthenticatedUser struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   string
}

// ProfileService はプロフィールに関するビジネスロジックを提供する。
type ProfileService struct {
	repo repository.ProfileRepository
}

// NewProfileService は ProfileService を生成する。
func NewProfileService(repo repository.ProfileRepository) *ProfileService {
	return &ProfileService{repo: repo}
}

// GetOrCreate は認証ユーザーのプロフィールを取得する。存在しなければ作成する。
//
// 認証情報(メール・表示名・アバター)で常に upsert するため、
// Supabase 側のプロフィール変更も次回アクセス時に反映される。
func (s *ProfileService) GetOrCreate(ctx context.Context, u AuthenticatedUser) (*model.Profile, error) {
	p := &model.Profile{
		ID:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
	}
	if err := s.repo.Upsert(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
