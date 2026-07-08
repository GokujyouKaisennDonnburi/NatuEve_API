package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// stubTagRepository は TagRepository のテスト用スタブ。
type stubTagRepository struct {
	tags []model.Tag
	err  error
}

func (s *stubTagRepository) List(
	_ context.Context,
) ([]model.Tag, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.tags, nil
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
