package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// ListRecipients 返却値。
	recipients        []model.EventRecipient
	listRecipientsErr error
	// ListMembers 返却値・引数記録。
	listMembers      []model.EventMember
	listMembersErr   error
	gotListMembersID uuid.UUID
}

func (s *stubEventJoinRepository) Join(_ context.Context, member *model.EventMember) error {
	s.gotMember = member
	if s.joinErr != nil {
		return s.joinErr
	}
	member.CreatedAt = s.joinCreatedAt
	return nil
}

func (s *stubEventJoinRepository) ListRecipients(_ context.Context, _ string) ([]model.EventRecipient, error) {
	return s.recipients, s.listRecipientsErr
}

func (s *stubEventJoinRepository) ListMembers(_ context.Context, eventID uuid.UUID) ([]model.EventMember, error) {
	s.gotListMembersID = eventID
	return s.listMembers, s.listMembersErr
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
			svc := NewEventJoinService(tt.stub, &stubEventRepository{})

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

func TestEventJoinServiceListMembers(t *testing.T) {
	// テスト用固定 UUID（再現性確保）。
	eventUID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	ownerUID := uuid.MustParse("b2c3d4e5-f6a8-8901-bcde-f23456789013")
	otherUID := uuid.MustParse("c3d4e5f6-a7b8-9012-cdef-345678901234")
	profileUID := uuid.NullUUID{UUID: uuid.MustParse("d4e5f6a7-b8c9-0123-defa-456789012345"), Valid: true}

	createdAt1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	createdAt2 := time.Date(2026, 7, 2, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name              string
		profileID         string
		eventID           string
		joinStub          *stubEventJoinRepository
		eventStub         *stubEventRepository
		wantValErr        bool
		wantForbiddenErr  bool
		wantNotFoundErr   bool
		wantErr           bool
		checkResp         func(t *testing.T, resp model.EventMemberListResponse)
		checkJoinedCalled func(t *testing.T, stub *stubEventJoinRepository)
	}{
		// 1. 正常: 主催者が取得 - 全フィールドが正しく返る
		{
			name:      "正常: 主催者が取得 - 全フィールドが正しく返る",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{
					{
						ID:          uuid.New(),
						EventID:     eventUID,
						ProfileID:   profileUID,
						Username:    "山田太郎",
						MailAddress: "yamada@example.com",
						PartySize:   3,
						CreatedAt:   createdAt1,
					},
					{
						ID:          uuid.New(),
						EventID:     eventUID,
						ProfileID:   uuid.NullUUID{}, // 匿名参加
						Username:    "匿名花子",
						MailAddress: "anon@example.com",
						PartySize:   1,
						CreatedAt:   createdAt2,
					},
				},
			},
			eventStub: &stubEventRepository{ownerProfileID: ownerUID.String()},
			checkResp: func(t *testing.T, resp model.EventMemberListResponse) {
				t.Helper()
				if len(resp.Members) != 2 {
					t.Fatalf("Members length: got %d, want 2", len(resp.Members))
				}
				if resp.TotalCount != 2 {
					t.Errorf("TotalCount: got %d, want 2", resp.TotalCount)
				}

				// 1人目: ログイン参加
				m0 := resp.Members[0]
				if m0.Username != "山田太郎" {
					t.Errorf("Members[0].Username: got %q, want %q", m0.Username, "山田太郎")
				}
				if m0.ProfileID == nil {
					t.Fatal("Members[0].ProfileID: got nil, want non-nil")
				}
				if *m0.ProfileID != profileUID.UUID {
					t.Errorf("Members[0].ProfileID: got %v, want %v", *m0.ProfileID, profileUID.UUID)
				}
				if m0.PartySize != 3 {
					t.Errorf("Members[0].PartySize: got %d, want 3", m0.PartySize)
				}
				if m0.MailAddress != "yamada@example.com" {
					t.Errorf("Members[0].MailAddress: got %q, want %q", m0.MailAddress, "yamada@example.com")
				}
				if !m0.CreatedAt.Equal(createdAt1) {
					t.Errorf("Members[0].CreatedAt: got %v, want %v", m0.CreatedAt, createdAt1)
				}

				// 2人目: 匿名参加
				m1 := resp.Members[1]
				if m1.Username != "匿名花子" {
					t.Errorf("Members[1].Username: got %q, want %q", m1.Username, "匿名花子")
				}
				if m1.ProfileID != nil {
					t.Errorf("Members[1].ProfileID: got %v, want nil（匿名）", *m1.ProfileID)
				}
				if m1.PartySize != 1 {
					t.Errorf("Members[1].PartySize: got %d, want 1", m1.PartySize)
				}
				if m1.MailAddress != "anon@example.com" {
					t.Errorf("Members[1].MailAddress: got %q, want %q", m1.MailAddress, "anon@example.com")
				}
				if !m1.CreatedAt.Equal(createdAt2) {
					t.Errorf("Members[1].CreatedAt: got %v, want %v", m1.CreatedAt, createdAt2)
				}
			},
		},
		// 2. 正常: 参加者0件 - 空配列と totalCount=0
		{
			name:      "正常: 参加者0件 - 空配列と totalCount=0",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{},
			},
			eventStub: &stubEventRepository{ownerProfileID: ownerUID.String()},
			checkResp: func(t *testing.T, resp model.EventMemberListResponse) {
				t.Helper()
				if resp.Members == nil {
					t.Fatal("Members: got nil, want empty slice (not nil)")
				}
				if len(resp.Members) != 0 {
					t.Errorf("Members length: got %d, want 0", len(resp.Members))
				}
				if resp.TotalCount != 0 {
					t.Errorf("TotalCount: got %d, want 0", resp.TotalCount)
				}
			},
		},
		// 3. 正常: 匿名参加者のみ - 全員の profileId が null
		{
			name:      "正常: 匿名参加者のみ - 全員の profileId が null",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{
					{
						ID:          uuid.New(),
						EventID:     eventUID,
						ProfileID:   uuid.NullUUID{}, // 匿名
						Username:    "匿名A",
						MailAddress: "anon-a@example.com",
						PartySize:   1,
						CreatedAt:   createdAt1,
					},
				},
			},
			eventStub: &stubEventRepository{ownerProfileID: ownerUID.String()},
			checkResp: func(t *testing.T, resp model.EventMemberListResponse) {
				t.Helper()
				if len(resp.Members) != 1 {
					t.Fatalf("Members length: got %d, want 1", len(resp.Members))
				}
				if resp.Members[0].ProfileID != nil {
					t.Errorf("ProfileID: got %v, want nil（匿名）", *resp.Members[0].ProfileID)
				}
				if resp.TotalCount != 1 {
					t.Errorf("TotalCount: got %d, want 1", resp.TotalCount)
				}
			},
		},
		// 4. 異常: 主催者以外 → ForbiddenError
		{
			name:             "異常: 主催者以外 → ForbiddenError",
			profileID:        otherUID.String(),
			eventID:          eventUID.String(),
			joinStub:         &stubEventJoinRepository{listMembers: nil},
			eventStub:        &stubEventRepository{ownerProfileID: ownerUID.String()},
			wantForbiddenErr: true,
		},
		// 5. 異常: イベントが存在しない → NotFoundError
		{
			name:      "異常: イベントが存在しない → NotFoundError",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub:  &stubEventJoinRepository{listMembers: nil},
			eventStub: &stubEventRepository{
				ownerProfileIDErr: fmt.Errorf("get event owner profile_id: %w", sql.ErrNoRows),
			},
			wantNotFoundErr: true,
		},
		// 6. 異常: eventID が不正な形式 → ValidationError
		{
			name:       "異常: eventID が不正な形式 → ValidationError",
			profileID:  ownerUID.String(),
			eventID:    "not-a-uuid",
			joinStub:   &stubEventJoinRepository{listMembers: nil},
			eventStub:  &stubEventRepository{},
			wantValErr: true,
		},
		// 7. 異常: profileID が不正な形式 → ForbiddenError (fail-closed)
		{
			name:             "異常: profileID が不正な形式 → ForbiddenError (fail-closed)",
			profileID:        "not-a-uuid",
			eventID:          eventUID.String(),
			joinStub:         &stubEventJoinRepository{listMembers: nil},
			eventStub:        &stubEventRepository{ownerProfileID: ownerUID.String()},
			wantForbiddenErr: true,
		},
		// 8. 異常: repo.ListMembers がエラー → エラー伝播
		{
			name:      "異常: repo.ListMembers がエラー → エラー伝播",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembersErr: errors.New("db error"),
			},
			eventStub: &stubEventRepository{ownerProfileID: ownerUID.String()},
			wantErr:   true,
		},
		// 9. 異常: repo.GetOwnerProfileID がエラー → エラー伝播
		{
			name:      "異常: repo.GetOwnerProfileID がエラー → エラー伝播",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub:  &stubEventJoinRepository{listMembers: nil},
			eventStub: &stubEventRepository{
				ownerProfileIDErr: errors.New("db error"),
			},
			wantErr: true,
		},
		// 10. 正常: 引数検証 - service が repo に正しい eventID を渡す
		{
			name:      "正常: 引数検証 - service が repo に正しい eventID を渡す",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{},
			},
			eventStub: &stubEventRepository{ownerProfileID: ownerUID.String()},
			checkResp: func(t *testing.T, resp model.EventMemberListResponse) {
				t.Helper()
				if len(resp.Members) != 0 {
					t.Errorf("Members length: got %d, want 0", len(resp.Members))
				}
				if resp.TotalCount != 0 {
					t.Errorf("TotalCount: got %d, want 0", resp.TotalCount)
				}
			},
			checkJoinedCalled: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()
				if stub.gotListMembersID != eventUID {
					t.Errorf("gotListMembersID: got %v, want %v", stub.gotListMembersID, eventUID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventJoinService(tt.joinStub, tt.eventStub)

			resp, err := svc.ListMembers(context.Background(), tt.profileID, tt.eventID)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				return
			case tt.wantForbiddenErr:
				_ = assertForbiddenError(t, err)
				return
			case tt.wantNotFoundErr:
				_ = assertNotFoundError(t, err)
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				// 想定外エラーは型なし（非ラップ）で伝播することを確認。
				var ve *ValidationError
				if errors.As(err, &ve) {
					t.Errorf("想定外エラーが ValidationError として伝播: %v", err)
				}
				var fe *ForbiddenError
				if errors.As(err, &fe) {
					t.Errorf("想定外エラーが ForbiddenError として伝播: %v", err)
				}
				var nfe *NotFoundError
				if errors.As(err, &nfe) {
					t.Errorf("想定外エラーが NotFoundError として伝播: %v", err)
				}
				return
			}

			assertNoErr(t, err)

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
			if tt.checkJoinedCalled != nil {
				tt.checkJoinedCalled(t, tt.joinStub)
			}
		})
	}
}
