package service

import (
	"context"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// TagService はタグに関するビジネスロジックを担当する。
type TagService struct {
	repo repository.TagRepository
}

// NewTagService はServiceを生成する。
func NewTagService(repo repository.TagRepository) *TagService {
	return &TagService{
		repo: repo,
	}
}

// List はタグ一覧を取得し、レスポンスDTOへ変換して返す。
func (s *TagService) List(
	ctx context.Context,
) ([]model.TagListResponse, error) {

	tags, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	resp := make([]model.TagListResponse, 0, len(tags))

	for _, tag := range tags {
		resp = append(resp, model.TagListResponse{
			ID:   tag.ID.String(),
			Name: tag.Name,
		})
	}

	return resp, nil
}
