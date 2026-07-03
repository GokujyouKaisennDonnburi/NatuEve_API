package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// stubEventJoinRepository は EventJoinRepository のテスト用スタブ。
type stubEventJoinRepository struct {
	// ExistsEvent 返却値。
	existsEventResult bool
	existsEventErr    error
	// ExistsMember 返却値と受け取った引数の記録。
	existsMemberResult    bool
	existsMemberErr       error
	gotExistsMemberID     uuid.UUID
	gotExistsMemberProfID uuid.NullUUID
	gotExistsMemberMail   string
	// CountMembers 返却値。
	countMembersResult int
	countMembersErr    error
	// GetCapacity 返却値。
	getCapacityResult int
	getCapacityErr    error
	// Join 返却値（joinCreatedAt は成功時に member.CreatedAt へセットする）。
	joinCreatedAt time.Time
	joinErr       error
	// 呼び出し時に Join へ渡された引数を記録する。
	gotMember *model.EventMember
}

func (s *stubEventJoinRepository) ExistsEvent(_ context.Context, _ uuid.UUID) (bool, error) {
	return s.existsEventResult, s.existsEventErr
}

func (s *stubEventJoinRepository) ExistsMember(
	_ context.Context,
	eventID uuid.UUID,
	profileID uuid.NullUUID,
	mailAddress string,
) (bool, error) {
	s.gotExistsMemberID = eventID
	s.gotExistsMemberProfID = profileID
	s.gotExistsMemberMail = mailAddress
	return s.existsMemberResult, s.existsMemberErr
}

func (s *stubEventJoinRepository) CountMembers(_ context.Context, _ uuid.UUID) (int, error) {
	return s.countMembersResult, s.countMembersErr
}

func (s *stubEventJoinRepository) GetCapacity(_ context.Context, _ uuid.UUID) (int, error) {
	return s.getCapacityResult, s.getCapacityErr
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

// defaultJoinStub は正常系テスト用のデフォルトスタブを返す。
func defaultJoinStub(createdAt time.Time) *stubEventJoinRepository {
	return &stubEventJoinRepository{
		existsEventResult:  true,
		existsMemberResult: false,
		getCapacityResult:  30,
		countMembersResult: 5,
		joinCreatedAt:      createdAt,
	}
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
	}

	tests := []struct {
		name         string
		stub         *stubEventJoinRepository
		profileID    uuid.NullUUID
		req          model.JoinEventRequest
		wantValErr   bool
		wantNotFound bool
		wantConflict bool
		wantErr      bool
		// 正常系: レスポンスの全フィールドを検証する。
		checkResp func(t *testing.T, resp model.JoinEventResponse)
		// 正常系: repo に渡った EventMember・ExistsMember 引数の内容を検証する。
		checkMember func(t *testing.T, stub *stubEventJoinRepository)
	}{
		// --- 正常系: ログイン参加 ---
		{
			name:      "正常: ログイン参加 - レスポンスの全フィールドが正しく返る",
			stub:      defaultJoinStub(createdAt),
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
				// ExistsMember に渡った引数を確認する。
				if stub.gotExistsMemberProfID != loggedInProfileID {
					t.Errorf("ExistsMember profileID: got %v, want %v", stub.gotExistsMemberProfID, loggedInProfileID)
				}
				if stub.gotExistsMemberMail != "yamada@example.com" {
					t.Errorf("ExistsMember mailAddress: got %q, want %q", stub.gotExistsMemberMail, "yamada@example.com")
				}
			},
		},
		// --- 正常系: 匿名参加 ---
		{
			name:      "正常: 匿名参加 - ProfileID が nil で返る",
			stub:      defaultJoinStub(createdAt),
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
				// 匿名時は ExistsMember に Valid=false の NullUUID が渡ること。
				if stub.gotExistsMemberProfID.Valid {
					t.Errorf("ExistsMember profileID.Valid: got true, want false（匿名）")
				}
			},
		},
		// --- 正常系: TrimSpace ---
		{
			name:      "正常: username・mailAddress の TrimSpace が反映される",
			stub:      defaultJoinStub(createdAt),
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "  山田太郎  ",
				MailAddress: "  yamada@example.com  ",
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
				// ExistsMember にもトリム済みの値が渡ること。
				if stub.gotExistsMemberMail != "yamada@example.com" {
					t.Errorf("ExistsMember mailAddress trim: got %q, want %q", stub.gotExistsMemberMail, "yamada@example.com")
				}
			},
		},
		// --- 正常系: 定員なし ---
		{
			name: "正常: capacity 0 は定員なしのため参加成功",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.getCapacityResult = 0
				s.countMembersResult = 999
				return s
			}(),
			profileID: loggedInProfileID,
			req:       validReq,
		},
		// --- バリデーションエラー ---
		{
			name:       "異常: username が空",
			stub:       defaultJoinStub(createdAt),
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "", MailAddress: "yamada@example.com"},
			wantValErr: true,
		},
		{
			name:       "異常: username が空白のみ",
			stub:       defaultJoinStub(createdAt),
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "   ", MailAddress: "yamada@example.com"},
			wantValErr: true,
		},
		{
			name:      "異常: username が 256 文字",
			stub:      defaultJoinStub(createdAt),
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
			stub:       defaultJoinStub(createdAt),
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "山田太郎", MailAddress: ""},
			wantValErr: true,
		},
		{
			name:       "異常: mailAddress の形式が不正",
			stub:       defaultJoinStub(createdAt),
			profileID:  loggedInProfileID,
			req:        model.JoinEventRequest{Username: "山田太郎", MailAddress: "not-an-email"},
			wantValErr: true,
		},
		// --- ビジネスチェックエラー ---
		{
			name: "異常: イベントが存在しない（NotFoundError）",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.existsEventResult = false
				return s
			}(),
			profileID:    loggedInProfileID,
			req:          validReq,
			wantNotFound: true,
		},
		{
			name: "異常: 既に参加済み（ConflictError）",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.existsMemberResult = true
				return s
			}(),
			profileID:    loggedInProfileID,
			req:          validReq,
			wantConflict: true,
		},
		{
			name: "異常: メール重複（ConflictError）",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				// 匿名参加でも同一 mail_address は重複と見なす。
				s.existsMemberResult = true
				return s
			}(),
			profileID:    anonymousProfileID,
			req:          validReq,
			wantConflict: true,
		},
		{
			name: "異常: 定員超過（ConflictError）",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.getCapacityResult = 30
				s.countMembersResult = 30
				return s
			}(),
			profileID:    loggedInProfileID,
			req:          validReq,
			wantConflict: true,
		},
		// --- リポジトリエラー伝播 ---
		{
			name: "異常: repo.ExistsEvent がエラーを返す",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.existsEventErr = errors.New("db error")
				return s
			}(),
			profileID: loggedInProfileID,
			req:       validReq,
			wantErr:   true,
		},
		{
			name: "異常: repo.Join がエラーを返す",
			stub: func() *stubEventJoinRepository {
				s := defaultJoinStub(createdAt)
				s.joinErr = errors.New("db error")
				return s
			}(),
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
				_ = assertConflictError(t, err)
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
