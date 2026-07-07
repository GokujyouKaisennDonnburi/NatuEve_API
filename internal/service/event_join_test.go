package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// stubEventJoinRepository は EventJoinRepository のテスト用スタブ。
type stubEventJoinRepository struct {
	// Join 返却値（joinCreatedAt は成功時に member.CreatedAt へセットする）。
	joinCreatedAt time.Time
	joinErr       error
	// 呼び出し時に Join へ渡された引数を記録する。
	gotMember *model.EventMember
}

func (s *stubEventJoinRepository) Join(_ context.Context, member *model.EventMember) error {
	s.gotMember = member
	if s.joinErr != nil {
		return s.joinErr
	}
	member.CreatedAt = s.joinCreatedAt
	return nil
}

// assertNotFoundError はテストヘルパー: err が *NotFoundError であることを確認する。
func assertNotFoundError(t *testing.T, err error) *NotFoundError {
	t.Helper()
	if err == nil {
		t.Fatal("NotFoundError を期待したが nil だった")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("*NotFoundError を期待したが %T だった: %v", err, err)
	}
	return nfe
}

// assertConflictError はテストヘルパー: err が *ConflictError であることを確認する。
func assertConflictError(t *testing.T, err error) *ConflictError {
	t.Helper()
	if err == nil {
		t.Fatal("ConflictError を期待したが nil だった")
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("*ConflictError を期待したが %T だった: %v", err, err)
	}
	return ce
}

func TestEventJoinServiceJoin(t *testing.T) {
	// 固定 UUID でテストの再現性を確保する。
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileUID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")
	// ログイン参加用の NullUUID（Valid=true）。
	loggedInProfileID := uuid.NullUUID{UUID: profileUID, Valid: true}
	// 匿名参加用の NullUUID（Valid=false）。
	anonymousProfileID := uuid.NullUUID{}
	createdAt := time.Date(2026, 6, 26, 4, 54, 35, 0, time.UTC)

	validReq := model.JoinEventRequest{
		Username:    "山田太郎",
		MailAddress: "yamada@example.com",
		PartySize:   1,
	}

	tests := []struct {
		name             string
		stub             *stubEventJoinRepository
		profileID        uuid.NullUUID
		req              model.JoinEventRequest
		wantValErr       bool
		wantNotFound     bool
		wantConflict     bool
		wantConflictCode string // wantConflict=true のとき検証する ConflictError.Code
		wantErr          bool
		// 正常系: レスポンスの全フィールドを検証する。
		checkResp func(t *testing.T, resp model.JoinEventResponse)
		// 正常系: repo に渡った EventMember の内容を検証する。
		checkMember func(t *testing.T, stub *stubEventJoinRepository)
	}{
		// --- 正常系: ログイン参加 ---
		{
			name:      "正常: ログイン参加 - レスポンスの全フィールドが正しく返る",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req:       validReq,
			checkResp: func(t *testing.T, resp model.JoinEventResponse) {
				t.Helper()
				if resp.EventID != eventID {
					t.Errorf("EventID: got %v, want %v", resp.EventID, eventID)
				}
				if resp.ProfileID == nil {
					t.Fatal("ProfileID: got nil, want non-nil")
				}
				if *resp.ProfileID != profileUID {
					t.Errorf("ProfileID: got %v, want %v", *resp.ProfileID, profileUID)
				}
				if resp.Username != "山田太郎" {
					t.Errorf("Username: got %q, want %q", resp.Username, "山田太郎")
				}
				if resp.MailAddress != "yamada@example.com" {
					t.Errorf("MailAddress: got %q, want %q", resp.MailAddress, "yamada@example.com")
				}
				if resp.PartySize != 1 {
					t.Errorf("PartySize: got %d, want %d", resp.PartySize, 1)
				}
				if !resp.CreatedAt.Equal(createdAt) {
					t.Errorf("CreatedAt: got %v, want %v", resp.CreatedAt, createdAt)
				}
			},
			checkMember: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()
				m := stub.gotMember
				if m == nil {
					t.Fatal("gotMember が nil")
				}
				if m.EventID != eventID {
					t.Errorf("EventMember.EventID: got %v, want %v", m.EventID, eventID)
				}
				if !m.ProfileID.Valid {
					t.Errorf("EventMember.ProfileID.Valid: got false, want true")
				}
				if m.ProfileID.UUID != profileUID {
					t.Errorf("EventMember.ProfileID.UUID: got %v, want %v", m.ProfileID.UUID, profileUID)
				}
				if m.Username != "山田太郎" {
					t.Errorf("EventMember.Username: got %q, want %q", m.Username, "山田太郎")
				}
				if m.MailAddress != "yamada@example.com" {
					t.Errorf("EventMember.MailAddress: got %q, want %q", m.MailAddress, "yamada@example.com")
				}
				if m.PartySize != 1 {
					t.Errorf("EventMember.PartySize: got %d, want 1", m.PartySize)
				}
			},
		},
		// --- 正常系: 匿名参加 ---
		{
			name:      "正常: 匿名参加 - ProfileID が nil で返る",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: anonymousProfileID,
			req:       validReq,
			checkResp: func(t *testing.T, resp model.JoinEventResponse) {
				t.Helper()
				if resp.ProfileID != nil {
					t.Errorf("ProfileID: got %v, want nil", resp.ProfileID)
				}
			},
			checkMember: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()
				m := stub.gotMember
				if m == nil {
					t.Fatal("gotMember が nil")
				}
				if m.ProfileID.Valid {
					t.Errorf("EventMember.ProfileID.Valid: got true, want false（匿名）")
				}
			},
		},
		// --- 正常系: TrimSpace ---
		{
			name:      "正常: username・mailAddress の TrimSpace が反映される",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "  山田太郎  ",
				MailAddress: "  yamada@example.com  ",
				PartySize:   1,
			},
			checkMember: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()
				m := stub.gotMember
				if m.Username != "山田太郎" {
					t.Errorf("Username trim: got %q, want %q", m.Username, "山田太郎")
				}
				if m.MailAddress != "yamada@example.com" {
					t.Errorf("MailAddress trim: got %q, want %q", m.MailAddress, "yamada@example.com")
				}
			},
		},
		// --- 正常系: 個人参加申請 ---
		{
			name: "正常: 1人参加申請 - PartySizeが正しく渡る",
			stub: &stubEventJoinRepository{
				joinCreatedAt: createdAt,
			},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				PartySize:   1,
			},
			checkMember: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()

				if stub.gotMember == nil {
					t.Fatal("gotMember が nil")
				}

				if stub.gotMember.PartySize != 1 {
					t.Errorf(
						"PartySize: got %d, want %d",
						stub.gotMember.PartySize,
						1,
					)
				}
			},
		},
		// --- 正常系: 複数人参加申請 ---
		{
			name: "正常: 複数人参加申請 - PartySizeが正しく渡る",
			stub: &stubEventJoinRepository{
				joinCreatedAt: createdAt,
			},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				PartySize:   5,
			},
			checkMember: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()

				if stub.gotMember == nil {
					t.Fatal("gotMember が nil")
				}

				if stub.gotMember.PartySize != 5 {
					t.Errorf(
						"PartySize: got %d, want %d",
						stub.gotMember.PartySize,
						5,
					)
				}
			},
		},
		// --- バリデーションエラー ---
		{
			name:       "異常: username が空",
			stub:       &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "", MailAddress: "yamada@example.com"},
			wantValErr: true,
		},
		{
			name:       "異常: username が空白のみ",
			stub:       &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "   ", MailAddress: "yamada@example.com"},
			wantValErr: true,
		},
		{
			name:      "異常: username が 256 文字",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: func() model.JoinEventRequest {
				runes := make([]rune, 256)
				for i := range runes {
					runes[i] = 'あ'
				}
				return model.JoinEventRequest{
					Username:    string(runes),
					MailAddress: "yamada@example.com",
				}
			}(),
			wantValErr: true,
		},
		{
			name:       "異常: mailAddress が空",
			stub:       &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "山田太郎", MailAddress: ""},
			wantValErr: true,
		},
		{
			name:       "異常: mailAddress の形式が不正",
			stub:       &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "山田太郎", MailAddress: "not-an-email"},
			wantValErr: true,
		},
		// --- repository の sentinel エラー変換 ---
		{
			name:         "異常: イベントが存在しない（NotFoundError）",
			stub:         &stubEventJoinRepository{joinErr: repository.ErrEventNotFound},
			profileID:    loggedInProfileID,
			req:          validReq,
			wantNotFound: true,
		},
		{
			name:             "異常: 既に参加済み（ConflictError）",
			stub:             &stubEventJoinRepository{joinErr: repository.ErrAlreadyJoined},
			profileID:        loggedInProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "already_joined",
		},
		{
			name:             "異常: メール重複 - UNIQUE 制約由来のラップ済みエラー（ConflictError）",
			stub:             &stubEventJoinRepository{joinErr: fmtWrap(repository.ErrAlreadyJoined)},
			profileID:        anonymousProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "already_joined",
		},
		{
			name:             "異常: 定員超過（ConflictError）",
			stub:             &stubEventJoinRepository{joinErr: repository.ErrEventCapacityFull},
			profileID:        loggedInProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "capacity_full",
		},
		{
			name:      "異常: PartySizeが0",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				PartySize:   0,
			},
			wantValErr: true,
		},
		{
			name:      "異常: PartySizeがマイナス",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				PartySize:   -1,
			},
			wantValErr: true,
		},
		// --- リポジトリエラー伝播 ---
		{
			name:      "異常: repo.Join が想定外のエラーを返す",
			stub:      &stubEventJoinRepository{joinErr: errors.New("db error")},
			profileID: loggedInProfileID,
			req:       validReq,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventJoinService(tt.stub)

			resp, err := svc.Join(context.Background(), eventID, tt.profileID, tt.req)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				return
			case tt.wantNotFound:
				_ = assertNotFoundError(t, err)
				return
			case tt.wantConflict:
				ce := assertConflictError(t, err)
				if tt.wantConflictCode != "" && ce.Code != tt.wantConflictCode {
					t.Errorf("ConflictError.Code: got %q, want %q", ce.Code, tt.wantConflictCode)
				}
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				return
			}

			assertNoErr(t, err)

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
			if tt.checkMember != nil {
				tt.checkMember(t, tt.stub)
			}
		})
	}
}

// fmtWrap は sentinel エラーを %w でラップした状態を再現するヘルパー。
// repository 実装はコンテキストを付けてラップするため、errors.Is で判定できることを確認する。
func fmtWrap(err error) error {
	return errors.Join(errors.New("event xxx"), err)
}
