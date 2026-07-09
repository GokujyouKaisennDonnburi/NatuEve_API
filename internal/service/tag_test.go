package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/google/uuid"
)

// stubTagRepository は TagRepository のテスト用スタブ。
type stubTagRepository struct {
	tags           []model.Tag
	created        model.Tag
	name           string
	normalizedName string
	err            error
}

func (s *stubTagRepository) List(
	_ context.Context,
) ([]model.Tag, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.tags, nil
}

func (s *stubTagRepository) Create(
	_ context.Context,
	name string,
	normalizedName string,
) (model.Tag, error) {
	s.name = name
	s.normalizedName = normalizedName

	if s.err != nil {
		return model.Tag{}, s.err
	}

	return s.created, nil
}

func TestTagServiceList(t *testing.T) {

	tagID := uuid.MustParse(
		"b2c3d4e5-f6a7-8901-bcde-f23456789012",
	)

	tests := []struct {
		name    string
		repo    *stubTagRepository
		wantErr bool
		check   func(
			t *testing.T,
			resp model.TagListResponse,
		)
	}{
		{
			name: "正常: タグ一覧取得",

			repo: &stubTagRepository{
				tags: []model.Tag{
					{
						ID:   tagID,
						Name: "外来生物",
					},
					{
						ID:   uuid.New(),
						Name: "植物",
					},
				},
			},

			check: func(
				t *testing.T,
				resp model.TagListResponse,
			) {
				t.Helper()

				if len(resp.Tags) != 2 {
					t.Fatalf(
						"Tags length: got %d want 1",
						len(resp.Tags),
					)
				}

				tag := resp.Tags[0]

				if tag.ID != tagID.String() {
					t.Errorf(
						"ID: got %s want %s",
						tag.ID,
						tagID.String(),
					)
				}

				if tag.Name != "外来生物" {
					t.Errorf(
						"Name: got %s want %s",
						tag.Name,
						"外来生物",
					)
				}
			},
		},

		{
			name: "正常: タグが存在しない",

			repo: &stubTagRepository{
				tags: []model.Tag{},
			},

			check: func(
				t *testing.T,
				resp model.TagListResponse,
			) {
				t.Helper()

				if resp.Tags == nil {
					t.Fatal(
						"Tags が nil",
					)
				}

				if len(resp.Tags) != 0 {
					t.Errorf(
						"Tags length: got %d want 0",
						len(resp.Tags),
					)
				}
			},
		},

		{
			name: "異常: Repositoryエラー",

			repo: &stubTagRepository{
				err: errors.New("database error"),
			},

			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTagService(tt.repo)
			resp, err := svc.List(
				context.Background(),
			)

			if tt.wantErr {
				if err == nil {
					t.Fatal(
						"errorを期待したがnil",
					)
				}
				return
			}

			if err != nil {
				t.Fatalf(
					"unexpected error: %v",
					err,
				)
			}

			if tt.check != nil {
				tt.check(
					t,
					resp,
				)
			}
		})
	}
}

func TestTagServiceCreate(t *testing.T) {
	tagID := uuid.MustParse(
		"b2c3d4e5-f6a7-8901-bcde-f23456789012",
	)

	tests := []struct {
		name    string
		repo    *stubTagRepository
		input   string
		wantErr bool
		check   func(
			t *testing.T,
			resp model.TagResponse,
			repo *stubTagRepository,
		)
	}{
		{
			name: "正常: タグ作成",

			repo: &stubTagRepository{
				created: model.Tag{
					ID:   tagID,
					Name: "外来生物",
				},
			},

			input: "外来生物",

			check: func(
				t *testing.T,
				resp model.TagResponse,
				_ *stubTagRepository,
			) {
				t.Helper()

				if resp.ID != tagID.String() {
					t.Errorf(
						"ID: got %s want %s",
						resp.ID,
						tagID.String(),
					)
				}

				if resp.Name != "外来生物" {
					t.Errorf(
						"Name: got %s want 外来生物",
						resp.Name,
					)
				}
			},
		},

		{
			name: "正常: 前後の空白を除去",

			repo: &stubTagRepository{
				created: model.Tag{
					ID:   tagID,
					Name: "外来生物",
				},
			},

			input: "  外来生物  ",

			check: func(
				t *testing.T,
				resp model.TagResponse,
				_ *stubTagRepository,
			) {
				t.Helper()

				if resp.Name != "外来生物" {
					t.Errorf("Name: got %s want 外来生物", resp.Name)
				}
			},
		},

		{
			name: "正常: 英字タグは入力文字列を保持する",

			repo: &stubTagRepository{
				created: model.Tag{
					ID:   tagID,
					Name: "Bird",
				},
			},

			input: "Bird",

			check: func(
				t *testing.T,
				resp model.TagResponse,
				_ *stubTagRepository,
			) {
				if resp.Name != "Bird" {
					t.Errorf(
						"Name: got %s want Bird",
						resp.Name,
					)
				}
			},
		},

		{
			name: "正常: 正規化キーは小文字化される",

			repo: &stubTagRepository{
				created: model.Tag{
					ID:   tagID,
					Name: "Bird",
				},
			},

			input: "Bird",

			check: func(
				t *testing.T,
				resp model.TagResponse,
				repo *stubTagRepository,
			) {
				if repo.name != "Bird" {
					t.Errorf("name: got %q want %q", repo.name, "Bird")
				}

				if repo.normalizedName != "bird" {
					t.Errorf("normalizedName: got %q want %q", repo.normalizedName, "bird")
				}

				if resp.Name != "Bird" {
					t.Errorf("response name: got %q want %q", resp.Name, "Bird")
				}
			},
		},

		{
			name: "異常: Repositoryエラー",

			repo: &stubTagRepository{
				err: repository.ErrDuplicateTag,
			},

			input: "外来生物",

			wantErr: true,
		},

		{
			name: "異常: タグ名が空",

			repo: &stubTagRepository{},

			input: "",

			wantErr: true,
		},

		{
			name: "異常: タグ名が空白のみ",

			repo: &stubTagRepository{},

			input: "    ",

			wantErr: true,
		},

		{
			name: "異常: タグ名が長すぎる",

			repo: &stubTagRepository{},

			input: strings.Repeat("あ", 31),

			wantErr: true,
		},

		{
			name: "異常: タグ重複",

			repo: &stubTagRepository{
				err: repository.ErrDuplicateTag,
			},

			input: "外来生物",

			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTagService(tt.repo)

			resp, err := svc.Create(
				context.Background(),
				tt.input,
			)

			if tt.wantErr {
				if err == nil {
					t.Fatal("errorを期待したがnil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, resp, tt.repo)
			}
		})
	}
}
