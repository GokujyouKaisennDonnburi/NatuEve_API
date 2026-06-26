package service

import (
	"context"
	"errors"
	"testing"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// stubProfileRepository は ProfileRepository のテスト用スタブ。
type stubProfileRepository struct {
	upsertCalled bool
	gotProfile   *model.Profile
	upsertErr    error
}

func (s *stubProfileRepository) GetByID(_ context.Context, _ string) (*model.Profile, error) {
	return nil, errors.New("not used")
}

func (s *stubProfileRepository) Upsert(_ context.Context, p *model.Profile) error {
	s.upsertCalled = true
	s.gotProfile = p
	return s.upsertErr
}

func TestProfileServiceGetOrCreate(t *testing.T) {
	user := AuthenticatedUser{
		ID:          "d290f1ee-6c54-4b01-90e6-d701748f0851",
		Email:       "user@example.com",
		DisplayName: "なちゅいべ太郎",
		AvatarURL:   "https://example.com/a.png",
	}

	tests := []struct {
		name      string
		upsertErr error
		wantErr   bool
	}{
		{name: "正常: Upsert が呼ばれプロフィールを返す", upsertErr: nil, wantErr: false},
		{name: "異常: Upsert のエラーが伝播する", upsertErr: errors.New("db error"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubProfileRepository{upsertErr: tt.upsertErr}
			svc := NewProfileService(repo)

			got, err := svc.GetOrCreate(context.Background(), user)

			if !repo.upsertCalled {
				t.Fatalf("Upsert が呼ばれていない")
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil だった")
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got.ID != user.ID || got.Email != user.Email ||
				got.DisplayName != user.DisplayName || got.AvatarURL != user.AvatarURL {
				t.Errorf("返り値が入力と一致しない: got %+v, want %+v", got, user)
			}
		})
	}
}
