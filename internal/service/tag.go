package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

var (
	// ErrEmptyTagName はタグ名が空の場合のエラー。
	ErrEmptyTagName = errors.New("タグ名を入力してください")
	// ErrTagNameTooLong はタグ名が上限文字数を超えた場合のエラー。
	ErrTagNameTooLong = errors.New("タグ名は30文字以内で入力してください")
	// ErrTagAlreadyExists は既に存在するタグを作成しようとした場合のエラー。
	ErrTagAlreadyExists = errors.New("タグが既に存在します")
)

const maxTagNameLength = 30

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
) (model.TagListResponse, error) {

	tags, err := s.repo.List(ctx)
	if err != nil {
		return model.TagListResponse{}, err
	}

	resp := make([]model.TagResponse, 0, len(tags))

	for _, tag := range tags {
		resp = append(resp, model.TagResponse{
			ID:   tag.ID.String(),
			Name: tag.Name,
		})
	}

	return model.TagListResponse{
		Tags: resp,
	}, nil
}

// Create は新しいタグを作成し、レスポンスDTOへ変換して返す。
func (s *TagService) Create(
	ctx context.Context,
	name string,
) (model.TagResponse, error) {

	name = norm.NFKC.String(name)
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)

	if name == "" {
		return model.TagResponse{}, ErrEmptyTagName
	}

	if utf8.RuneCountInString(name) > maxTagNameLength {
		return model.TagResponse{}, ErrTagNameTooLong
	}

	tag, err := s.repo.Create(
		ctx,
		name,
	)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateTag) {
			return model.TagResponse{}, ErrTagAlreadyExists
		}
		return model.TagResponse{}, err
	}

	return model.TagResponse{
		ID:   tag.ID.String(),
		Name: tag.Name,
	}, nil
}
