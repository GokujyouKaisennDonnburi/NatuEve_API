package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// Leave 返却値・引数記録。
	leaveCreatedAt    time.Time
	leaveErr          error
	gotLeaveEventID   uuid.UUID
	gotLeaveProfileID uuid.UUID
	// ListRecipients 返却値。
	recipients        []model.EventRecipient
	listRecipientsErr error
	// ListMembers 返却値・引数記録。
	listMembers      []model.EventMember
	listMembersErr   error
	gotListMembersID uuid.UUID
	// GetMemberByProfile 返却値・引数記録。
	memberByProfile             model.EventMember
	memberByProfileErr          error
	gotMemberByProfileEventID   uuid.UUID
	gotMemberByProfileProfileID uuid.UUID
	// Absence 返却値・引数記録。
	absenceCreatedAt    time.Time
	absenceErr          error
	gotAbsenceEventID   uuid.UUID
	gotAbsenceProfileID uuid.UUID
	gotAbsenceReason    string
	gotAbsenceDetail    string
	gotAbsenceSubject   string
	gotAbsenceBody      string
}

func (s *stubEventJoinRepository) Join(_ context.Context, member *model.EventMember) error {
	s.gotMember = member
	if s.joinErr != nil {
		return s.joinErr
	}
	member.CreatedAt = s.joinCreatedAt
	return nil
}

func (s *stubEventJoinRepository) Leave(_ context.Context, eventID, profileID uuid.UUID) (time.Time, error) {
	s.gotLeaveEventID = eventID
	s.gotLeaveProfileID = profileID
	if s.leaveErr != nil {
		return time.Time{}, s.leaveErr
	}
	return s.leaveCreatedAt, nil
}

func (s *stubEventJoinRepository) ListRecipients(_ context.Context, _ uuid.UUID) ([]model.EventRecipient, error) {
	return s.recipients, s.listRecipientsErr
}

func (s *stubEventJoinRepository) ListMembers(_ context.Context, eventID uuid.UUID) ([]model.EventMember, error) {
	s.gotListMembersID = eventID
	return s.listMembers, s.listMembersErr
}

func (s *stubEventJoinRepository) GetMemberByProfile(
	_ context.Context,
	eventID, profileID uuid.UUID,
) (model.EventMember, error) {
	s.gotMemberByProfileEventID = eventID
	s.gotMemberByProfileProfileID = profileID
	return s.memberByProfile, s.memberByProfileErr
}

func (s *stubEventJoinRepository) Absence(
	_ context.Context,
	eventID, profileID uuid.UUID,
	reason, detail, subject, body string,
) (time.Time, error) {
	s.gotAbsenceEventID = eventID
	s.gotAbsenceProfileID = profileID
	s.gotAbsenceReason = reason
	s.gotAbsenceDetail = detail
	s.gotAbsenceSubject = subject
	s.gotAbsenceBody = body
	if s.absenceErr != nil {
		return time.Time{}, s.absenceErr
	}
	return s.absenceCreatedAt, nil
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
		Participants: []model.ParticipantInput{
			{Category: "大人", HeadCount: 1},
		},
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
				if len(resp.Participants) != 1 {
					t.Fatalf("Participants: got %d件, want 1件", len(resp.Participants))
				}
				if resp.Participants[0].Category != "大人" {
					t.Errorf("Participants[0].Category: got %q, want %q", resp.Participants[0].Category, "大人")
				}
				if resp.Participants[0].HeadCount != 1 {
					t.Errorf("Participants[0].HeadCount: got %d, want 1", resp.Participants[0].HeadCount)
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
					return
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
					return
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
				Participants: []model.ParticipantInput{
					{Category: "  大人  ", HeadCount: 1},
				},
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
				if len(m.Categories) != 1 {
					t.Fatalf("Categories: got %d件, want 1件", len(m.Categories))
				}
				if m.Categories[0].Category != "大人" {
					t.Errorf("Category trim: got %q, want %q", m.Categories[0].Category, "大人")
				}
			},
		},
		// --- 正常系: 個人参加申請 ---
		{
			name: "正常: 1カテゴリ1名 - 内訳の合計が PartySize になる",
			stub: &stubEventJoinRepository{
				joinCreatedAt: createdAt,
			},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "大人", HeadCount: 1},
				},
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
		// --- 正常系: 複数カテゴリの申請 ---
		{
			name: "正常: 複数カテゴリ - 内訳の合計が PartySize になり内訳がそのまま渡る",
			stub: &stubEventJoinRepository{
				joinCreatedAt: createdAt,
			},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "大人", HeadCount: 2},
					{Category: "学生", HeadCount: 3},
				},
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
				if len(stub.gotMember.Categories) != 2 {
					t.Fatalf("Categories: got %d件, want 2件", len(stub.gotMember.Categories))
				}
				for i, want := range []model.MemberCategory{
					{Category: "大人", HeadCount: 2},
					{Category: "学生", HeadCount: 3},
				} {
					got := stub.gotMember.Categories[i]
					if got.Category != want.Category || got.HeadCount != want.HeadCount {
						t.Errorf(
							"Categories[%d]: got {%q %d}, want {%q %d}",
							i, got.Category, got.HeadCount, want.Category, want.HeadCount,
						)
					}
				}
			},
		},
		// --- 正常系: レスポンスの内訳はカテゴリ名の昇順 ---
		{
			name: "正常: participants はカテゴリ名の昇順で返る",
			stub: &stubEventJoinRepository{
				joinCreatedAt: createdAt,
			},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "Student", HeadCount: 1},
					{Category: "Adult", HeadCount: 2},
				},
			},
			checkResp: func(t *testing.T, resp model.JoinEventResponse) {
				t.Helper()
				want := []model.ParticipantResponse{
					{Category: "Adult", HeadCount: 2},
					{Category: "Student", HeadCount: 1},
				}
				if len(resp.Participants) != len(want) {
					t.Fatalf("Participants: got %d件, want %d件", len(resp.Participants), len(want))
				}
				for i := range want {
					if resp.Participants[i] != want[i] {
						t.Errorf("Participants[%d]: got %+v, want %+v", i, resp.Participants[i], want[i])
					}
				}
				if resp.PartySize != 3 {
					t.Errorf("PartySize: got %d, want 3", resp.PartySize)
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
			name:             "異常: イベントがキャンセル済み（ConflictError）",
			stub:             &stubEventJoinRepository{joinErr: repository.ErrEventCancelled},
			profileID:        loggedInProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "event_cancelled",
		},
		{
			name:             "異常: 申込期限経過後（ConflictError deadline_passed）",
			stub:             &stubEventJoinRepository{joinErr: repository.ErrDeadlinePassed},
			profileID:        loggedInProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "deadline_passed",
		},
		{
			name:             "異常: イベント終了後（ConflictError event_ended）",
			stub:             &stubEventJoinRepository{joinErr: repository.ErrEventEnded},
			profileID:        loggedInProfileID,
			req:              validReq,
			wantConflict:     true,
			wantConflictCode: "event_ended",
		},
		{
			name:      "異常: participants が空",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:     "山田太郎",
				MailAddress:  "yamada@example.com",
				Participants: []model.ParticipantInput{},
			},
			wantValErr: true,
		},
		{
			name:      "異常: headCount が0",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "大人", HeadCount: 0},
				},
			},
			wantValErr: true,
		},
		{
			name:      "異常: headCount がマイナス",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "大人", HeadCount: -1},
				},
			},
			wantValErr: true,
		},
		{
			name:      "異常: カテゴリが空白のみ",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "   ", HeadCount: 1},
				},
			},
			wantValErr: true,
		},
		{
			name:      "異常: 同一カテゴリの重複（大文字小文字違いも同一とみなす）",
			stub:      &stubEventJoinRepository{joinCreatedAt: createdAt},
			profileID: loggedInProfileID,
			req: model.JoinEventRequest{
				Username:    "山田太郎",
				MailAddress: "yamada@example.com",
				Participants: []model.ParticipantInput{
					{Category: "Adult", HeadCount: 1},
					{Category: "adult", HeadCount: 2},
				},
			},
			wantValErr: true,
		},
		{
			name:       "異常: イベントに存在しないカテゴリ（ValidationError に変換）",
			stub:       &stubEventJoinRepository{joinErr: repository.ErrCategoryNotFound},
			profileID:  loggedInProfileID,
			req:        validReq,
			wantValErr: true,
		},
		{
			name:       "異常: repository が重複カテゴリを検出（ValidationError に変換）",
			stub:       &stubEventJoinRepository{joinErr: fmtWrap(repository.ErrDuplicateCategory)},
			profileID:  loggedInProfileID,
			req:        validReq,
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
			svc := NewEventJoinService(tt.stub, &stubEventRepository{}, nil)

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

func TestEventJoinServiceLeave(t *testing.T) {
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")
	createdAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		stub             *stubEventJoinRepository
		wantNotFound     bool
		wantConflict     bool
		wantConflictCode string // wantConflict=true のとき検証する ConflictError.Code
		wantErr          bool
		checkResp        func(t *testing.T, resp model.LeaveEventResponse)
	}{
		{
			name: "正常: 参加取消 - レスポンスの全フィールドが正しく返る",
			stub: &stubEventJoinRepository{leaveCreatedAt: createdAt},
			checkResp: func(t *testing.T, resp model.LeaveEventResponse) {
				t.Helper()
				if resp.EventID != eventID {
					t.Errorf("EventID: got %v, want %v", resp.EventID, eventID)
				}
				if resp.ProfileID != profileID {
					t.Errorf("ProfileID: got %v, want %v", resp.ProfileID, profileID)
				}
				if resp.Action != "leave" {
					t.Errorf("Action: got %q, want %q", resp.Action, "leave")
				}
				if !resp.CreatedAt.Equal(createdAt) {
					t.Errorf("CreatedAt: got %v, want %v", resp.CreatedAt, createdAt)
				}
			},
		},
		{
			name:         "異常: イベントが存在しない（NotFoundError）",
			stub:         &stubEventJoinRepository{leaveErr: repository.ErrEventNotFound},
			wantNotFound: true,
		},
		{
			name:         "異常: 未参加（NotFoundError）",
			stub:         &stubEventJoinRepository{leaveErr: repository.ErrNotJoined},
			wantNotFound: true,
		},
		{
			name:         "異常: sentinel をラップ済みでも NotFoundError に変換される",
			stub:         &stubEventJoinRepository{leaveErr: fmtWrap(repository.ErrNotJoined)},
			wantNotFound: true,
		},
		{
			name:             "異常: 申込期限経過後は ConflictError（deadline_passed）を返す",
			stub:             &stubEventJoinRepository{leaveErr: repository.ErrDeadlinePassed},
			wantConflict:     true,
			wantConflictCode: "deadline_passed",
		},
		{
			name:    "異常: repo.Leave が想定外のエラーを返す",
			stub:    &stubEventJoinRepository{leaveErr: errors.New("db error")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventJoinService(tt.stub, &stubEventRepository{}, nil)

			resp, err := svc.Leave(context.Background(), eventID, profileID)

			switch {
			case tt.wantNotFound:
				_ = assertNotFoundError(t, err)
				return
			case tt.wantConflict:
				ce := assertConflictError(t, err)
				if ce.Code != tt.wantConflictCode {
					t.Errorf("ConflictError.Code: got %q, want %q", ce.Code, tt.wantConflictCode)
				}
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				var nfe *NotFoundError
				if errors.As(err, &nfe) {
					t.Errorf("想定外エラーが NotFoundError として伝播: %v", err)
				}
				return
			}

			assertNoErr(t, err)

			// service が repo に正しい引数を渡していることを確認する。
			if tt.stub.gotLeaveEventID != eventID {
				t.Errorf("gotLeaveEventID: got %v, want %v", tt.stub.gotLeaveEventID, eventID)
			}
			if tt.stub.gotLeaveProfileID != profileID {
				t.Errorf("gotLeaveProfileID: got %v, want %v", tt.stub.gotLeaveProfileID, profileID)
			}
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}

func TestEventJoinServiceGetMyApplication(t *testing.T) {
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")
	createdAt := time.Date(2026, 8, 1, 12, 34, 56, 0, time.UTC)

	tests := []struct {
		name            string
		stub            *stubEventJoinRepository
		wantNotFound    bool
		wantNotFoundMsg string
		wantErr         bool
		checkResp       func(t *testing.T, resp model.MyEventApplicationResponse)
	}{
		{
			name: "正常: participants がカテゴリ名昇順で partySize・createdAt・username・mailAddress が詰まる",
			stub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{
					Username:    "山田太郎",
					MailAddress: "yamada@example.com",
					PartySize:   3,
					Categories: []model.MemberCategory{
						{Category: "学生", HeadCount: 1},
						{Category: "大人", HeadCount: 2},
					},
					CreatedAt: createdAt,
				},
			},
			checkResp: func(t *testing.T, resp model.MyEventApplicationResponse) {
				t.Helper()
				if resp.EventID != eventID {
					t.Errorf("EventID: got %v, want %v", resp.EventID, eventID)
				}
				if resp.Username != "山田太郎" {
					t.Errorf("Username: got %q, want %q", resp.Username, "山田太郎")
				}
				if resp.MailAddress != "yamada@example.com" {
					t.Errorf("MailAddress: got %q, want %q", resp.MailAddress, "yamada@example.com")
				}
				if resp.PartySize != 3 {
					t.Errorf("PartySize: got %d, want 3", resp.PartySize)
				}
				want := []model.ParticipantResponse{
					{Category: "大人", HeadCount: 2},
					{Category: "学生", HeadCount: 1},
				}
				if len(resp.Participants) != len(want) {
					t.Fatalf("Participants: got %d件, want %d件", len(resp.Participants), len(want))
				}
				for i := range want {
					if resp.Participants[i] != want[i] {
						t.Errorf("Participants[%d]: got %+v, want %+v", i, resp.Participants[i], want[i])
					}
				}
				if !resp.CreatedAt.Equal(createdAt) {
					t.Errorf("CreatedAt: got %v, want %v", resp.CreatedAt, createdAt)
				}
			},
		},
		{
			name:            "異常: イベントが存在しない（NotFoundError）",
			stub:            &stubEventJoinRepository{memberByProfileErr: repository.ErrEventNotFound},
			wantNotFound:    true,
			wantNotFoundMsg: "イベントが見つかりません",
		},
		{
			name:            "異常: 未申込（NotFoundError）",
			stub:            &stubEventJoinRepository{memberByProfileErr: repository.ErrNotJoined},
			wantNotFound:    true,
			wantNotFoundMsg: "このイベントに参加していません",
		},
		{
			name:    "異常: repo.GetMemberByProfile が想定外のエラーを返す",
			stub:    &stubEventJoinRepository{memberByProfileErr: errors.New("db error")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventJoinService(tt.stub, &stubEventRepository{}, nil)

			resp, err := svc.GetMyApplication(context.Background(), eventID, profileID)

			switch {
			case tt.wantNotFound:
				nfe := assertNotFoundError(t, err)
				if tt.wantNotFoundMsg != "" && nfe.Message != tt.wantNotFoundMsg {
					t.Errorf("NotFoundError.Message: got %q, want %q", nfe.Message, tt.wantNotFoundMsg)
				}
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				var nfe *NotFoundError
				if errors.As(err, &nfe) {
					t.Errorf("想定外エラーが NotFoundError として伝播: %v", err)
				}
				return
			}

			assertNoErr(t, err)

			// service が repo に正しい引数を渡していることを確認する。
			if tt.stub.gotMemberByProfileEventID != eventID {
				t.Errorf("gotMemberByProfileEventID: got %v, want %v", tt.stub.gotMemberByProfileEventID, eventID)
			}
			if tt.stub.gotMemberByProfileProfileID != profileID {
				t.Errorf("gotMemberByProfileProfileID: got %v, want %v", tt.stub.gotMemberByProfileProfileID, profileID)
			}
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}

func TestEventJoinServiceAbsence(t *testing.T) {
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")
	createdAt := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// 255文字のタイトル（「あ」×255）。
	title255 := strings.Repeat("あ", 255)

	tests := []struct {
		name             string
		joinStub         *stubEventJoinRepository
		eventStub        *stubEventRepository
		req              model.AbsenceEventRequest
		wantValErr       bool
		wantNotFound     bool
		wantConflict     bool
		wantConflictCode string
		wantErr          bool
		checkResp        func(t *testing.T, resp model.AbsenceEventResponse)
		checkRepo        func(t *testing.T, joinStub *stubEventJoinRepository)
	}{
		{
			name: "正常: レスポンスの全フィールドが正しく返り、文面にイベント名・参加者名・理由ラベル・詳細が入る",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req: model.AbsenceEventRequest{
				Reason: model.AbsenceReasonIllness,
				Detail: "熱が出たため",
			},
			checkResp: func(t *testing.T, resp model.AbsenceEventResponse) {
				t.Helper()
				if resp.EventID != eventID {
					t.Errorf("EventID: got %v, want %v", resp.EventID, eventID)
				}
				if resp.ProfileID != profileID {
					t.Errorf("ProfileID: got %v, want %v", resp.ProfileID, profileID)
				}
				if resp.Action != "absence" {
					t.Errorf("Action: got %q, want %q", resp.Action, "absence")
				}
				if resp.Reason == nil || *resp.Reason != "illness" {
					t.Errorf("Reason: got %v, want %q", resp.Reason, "illness")
				}
				if resp.Detail == nil || *resp.Detail != "熱が出たため" {
					t.Errorf("Detail: got %v, want %q", resp.Detail, "熱が出たため")
				}
				if !resp.CreatedAt.Equal(createdAt) {
					t.Errorf("CreatedAt: got %v, want %v", resp.CreatedAt, createdAt)
				}
			},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				if joinStub.gotAbsenceEventID != eventID {
					t.Errorf("gotAbsenceEventID: got %v, want %v", joinStub.gotAbsenceEventID, eventID)
				}
				if joinStub.gotAbsenceProfileID != profileID {
					t.Errorf("gotAbsenceProfileID: got %v, want %v", joinStub.gotAbsenceProfileID, profileID)
				}
				if joinStub.gotAbsenceReason != "illness" {
					t.Errorf("gotAbsenceReason: got %q, want %q", joinStub.gotAbsenceReason, "illness")
				}
				if joinStub.gotAbsenceDetail != "熱が出たため" {
					t.Errorf("gotAbsenceDetail: got %q, want %q", joinStub.gotAbsenceDetail, "熱が出たため")
				}
				wantSubject := "【欠席連絡】「たき火観察会」への参加キャンセル"
				if joinStub.gotAbsenceSubject != wantSubject {
					t.Errorf("gotAbsenceSubject: got %q, want %q", joinStub.gotAbsenceSubject, wantSubject)
				}
				wantBody := "イベント名：たき火観察会\n参加者名：山田太郎\n欠席理由：体調不良\n詳細：熱が出たため"
				if joinStub.gotAbsenceBody != wantBody {
					t.Errorf("gotAbsenceBody: got %q, want %q", joinStub.gotAbsenceBody, wantBody)
				}
			},
		},
		{
			name: "正常: detail 未入力 - 本文に詳細行が入らず response.Detail は nil",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req:       model.AbsenceEventRequest{Reason: model.AbsenceReasonWeatherTransport},
			checkResp: func(t *testing.T, resp model.AbsenceEventResponse) {
				t.Helper()
				if resp.Detail != nil {
					t.Errorf("Detail: got %q, want nil", *resp.Detail)
				}
			},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				wantBody := "イベント名：たき火観察会\n参加者名：山田太郎\n欠席理由：天候・交通"
				if joinStub.gotAbsenceBody != wantBody {
					t.Errorf("gotAbsenceBody: got %q, want %q", joinStub.gotAbsenceBody, wantBody)
				}
			},
		},
		{
			name: "正常: detail 前後の空白が trim されて repo へ渡る",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req: model.AbsenceEventRequest{
				Reason: model.AbsenceReasonFamily,
				Detail: "  兄弟の入学式に合わせる  ",
			},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				if joinStub.gotAbsenceDetail != "兄弟の入学式に合わせる" {
					t.Errorf("gotAbsenceDetail trim: got %q, want %q", joinStub.gotAbsenceDetail, "兄弟の入学式に合わせる")
				}
			},
		},
		{
			name: "正常: 各欠席理由のラベルが文面に反映される",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req:       model.AbsenceEventRequest{Reason: model.AbsenceReasonOther},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				if !strings.Contains(joinStub.gotAbsenceBody, "欠席理由：その他") {
					t.Errorf("gotAbsenceBody に「欠席理由：その他」が含まれていない: %q", joinStub.gotAbsenceBody)
				}
			},
		},
		{
			name: "正常: title が255文字でも件名が255文字以内に収まる",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: title255},
			req:       model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				if n := len([]rune(joinStub.gotAbsenceSubject)); n > 255 {
					t.Errorf("gotAbsenceSubject の rune 数 = %d, want <= 255", n)
				}
				if !strings.HasPrefix(joinStub.gotAbsenceSubject, "【欠席連絡】「") {
					t.Errorf("gotAbsenceSubject が期待の形式で始まっていない: %q", joinStub.gotAbsenceSubject)
				}
				if !strings.HasSuffix(joinStub.gotAbsenceSubject, "」への参加キャンセル") {
					t.Errorf("gotAbsenceSubject が期待の形式で終わっていない: %q", joinStub.gotAbsenceSubject)
				}
			},
		},
		{
			name: "正常: 詳細が200文字ちょうどは受け付けられる",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req: model.AbsenceEventRequest{
				Reason: model.AbsenceReasonIllness,
				Detail: strings.Repeat("あ", 200),
			},
		},
		{
			name: "異常: reason が4値以外",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
			},
			eventStub:  &stubEventRepository{title: "たき火観察会"},
			req:        model.AbsenceEventRequest{Reason: model.AbsenceReason("sick")},
			wantValErr: true,
		},
		{
			name: "正常: reason 未指定 - repo へ空文字が渡り、本文は「記載なし」、response.Reason は nil",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req:       model.AbsenceEventRequest{Detail: "熱が出たため"},
			checkResp: func(t *testing.T, resp model.AbsenceEventResponse) {
				t.Helper()
				if resp.Reason != nil {
					t.Errorf("Reason: got %q, want nil", *resp.Reason)
				}
			},
			checkRepo: func(t *testing.T, joinStub *stubEventJoinRepository) {
				t.Helper()
				if joinStub.gotAbsenceReason != "" {
					t.Errorf("gotAbsenceReason: got %q, want empty", joinStub.gotAbsenceReason)
				}
				wantBody := "イベント名：たき火観察会\n参加者名：山田太郎\n欠席理由：記載なし\n詳細：熱が出たため"
				if joinStub.gotAbsenceBody != wantBody {
					t.Errorf("gotAbsenceBody: got %q, want %q", joinStub.gotAbsenceBody, wantBody)
				}
			},
		},
		{
			name: "異常: detail が201文字",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req: model.AbsenceEventRequest{
				Reason: model.AbsenceReasonIllness,
				Detail: strings.Repeat("あ", 201),
			},
			wantValErr: true,
		},
		{
			name:         "異常: 未参加（NotFoundError）",
			joinStub:     &stubEventJoinRepository{memberByProfileErr: repository.ErrNotJoined},
			eventStub:    &stubEventRepository{title: "たき火観察会"},
			req:          model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantNotFound: true,
		},
		{
			name:         "異常: イベントが存在しない（NotFoundError）",
			joinStub:     &stubEventJoinRepository{memberByProfileErr: repository.ErrEventNotFound},
			eventStub:    &stubEventRepository{title: "たき火観察会"},
			req:          model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantNotFound: true,
		},
		{
			name: "異常: GetTitle がイベント不存在（NotFoundError）",
			joinStub: &stubEventJoinRepository{
				memberByProfile:  model.EventMember{Username: "山田太郎"},
				absenceCreatedAt: createdAt,
			},
			eventStub:    &stubEventRepository{titleErr: fmtWrap(repository.ErrEventNotFound)},
			req:          model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantNotFound: true,
		},
		{
			name: "異常: GetTitle が想定外のエラーを返す",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
			},
			eventStub: &stubEventRepository{titleErr: fmtWrap(errors.New("db error"))},
			req:       model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantErr:   true,
		},
		{
			name: "異常: 申込期限前（ConflictError before_deadline）",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
				absenceErr:      repository.ErrAbsenceBeforeDeadline,
			},
			eventStub:        &stubEventRepository{title: "たき火観察会"},
			req:              model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantConflict:     true,
			wantConflictCode: "before_deadline",
		},
		{
			name: "異常: イベント終了後（ConflictError event_ended）",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
				absenceErr:      repository.ErrEventEnded,
			},
			eventStub:        &stubEventRepository{title: "たき火観察会"},
			req:              model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantConflict:     true,
			wantConflictCode: "event_ended",
		},
		{
			name: "異常: イベント取消済み（ConflictError event_cancelled）",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
				absenceErr:      repository.ErrEventCancelled,
			},
			eventStub:        &stubEventRepository{title: "たき火観察会"},
			req:              model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantConflict:     true,
			wantConflictCode: "event_cancelled",
		},
		{
			name: "異常: sentinel をラップ済みでも ConflictError に変換される",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
				absenceErr:      fmtWrap(repository.ErrEventEnded),
			},
			eventStub:        &stubEventRepository{title: "たき火観察会"},
			req:              model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantConflict:     true,
			wantConflictCode: "event_ended",
		},
		{
			name: "異常: repo.Absence が想定外のエラーを返す",
			joinStub: &stubEventJoinRepository{
				memberByProfile: model.EventMember{Username: "山田太郎"},
				absenceErr:      errors.New("db error"),
			},
			eventStub: &stubEventRepository{title: "たき火観察会"},
			req:       model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wakeCalled := false
			wake := func() { wakeCalled = true }
			svc := NewEventJoinService(tt.joinStub, tt.eventStub, wake)

			resp, err := svc.Absence(context.Background(), eventID, profileID, tt.req)

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

			if !wakeCalled {
				t.Error("wake が呼ばれていない（outbox 予約後の起床通知が行われていない）")
			}
			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
			if tt.checkRepo != nil {
				tt.checkRepo(t, tt.joinStub)
			}
		})
	}
}

func TestEventJoinServiceAbsence_WakeNil(t *testing.T) {
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")

	svc := NewEventJoinService(
		&stubEventJoinRepository{memberByProfile: model.EventMember{Username: "山田太郎"}},
		&stubEventRepository{title: "たき火観察会"},
		nil,
	)

	_, err := svc.Absence(
		context.Background(),
		eventID,
		profileID,
		model.AbsenceEventRequest{Reason: model.AbsenceReasonIllness},
	)
	assertNoErr(t, err)
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
		name                   string
		profileID              string
		eventID                string
		joinStub               *stubEventJoinRepository
		eventStub              *stubEventRepository
		wantValErr             bool
		wantForbiddenErr       bool
		wantErr                bool
		checkResp              func(t *testing.T, resp model.EventMemberListResponse)
		checkListMembersCalled func(t *testing.T, stub *stubEventJoinRepository)
	}{
		// 1. 正常: 主催者が取得 - 全フィールドが正しく返る
		{
			name:      "正常: 主催者が取得 - 全フィールドが正しく返る",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{
					{
						EventID:     eventUID,
						ProfileID:   profileUID,
						Username:    "山田太郎",
						MailAddress: "yamada@example.com",
						PartySize:   3,
						CreatedAt:   createdAt1,
						Profile: &model.ProfileSummary{
							ID:          profileUID.UUID.String(),
							DisplayName: "なちゅいべ太郎",
							AvatarURL:   "https://example.com/avatar.png",
						},
					},
					{
						EventID:     eventUID,
						ProfileID:   uuid.NullUUID{}, // 匿名参加
						Username:    "匿名花子",
						MailAddress: "anon@example.com",
						PartySize:   5,
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
				// PartySize 3 + 5 = 8（TotalCount と区別できる値にしている）。
				if resp.TotalMembers != 8 {
					t.Errorf("TotalMembers: got %d, want 8", resp.TotalMembers)
				}

				// 1人目: ログイン参加
				m0 := resp.Members[0]
				if m0.Username != "山田太郎" {
					t.Errorf("Members[0].Username: got %q, want %q", m0.Username, "山田太郎")
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
				if m0.Profile == nil {
					t.Fatal("Members[0].Profile: got nil, want non-nil")
				}
				if m0.Profile.ID != profileUID.UUID.String() {
					t.Errorf("Members[0].Profile.ID: got %q, want %q", m0.Profile.ID, profileUID.UUID.String())
				}
				if m0.Profile.DisplayName != "なちゅいべ太郎" {
					t.Errorf("Members[0].Profile.DisplayName: got %q, want %q", m0.Profile.DisplayName, "なちゅいべ太郎")
				}
				if m0.Profile.AvatarURL != "https://example.com/avatar.png" {
					t.Errorf("Members[0].Profile.AvatarURL: got %q, want %q", m0.Profile.AvatarURL, "https://example.com/avatar.png")
				}

				// 2人目: 匿名参加
				m1 := resp.Members[1]
				if m1.Username != "匿名花子" {
					t.Errorf("Members[1].Username: got %q, want %q", m1.Username, "匿名花子")
				}
				if m1.PartySize != 5 {
					t.Errorf("Members[1].PartySize: got %d, want 5", m1.PartySize)
				}
				if m1.MailAddress != "anon@example.com" {
					t.Errorf("Members[1].MailAddress: got %q, want %q", m1.MailAddress, "anon@example.com")
				}
				if !m1.CreatedAt.Equal(createdAt2) {
					t.Errorf("Members[1].CreatedAt: got %v, want %v", m1.CreatedAt, createdAt2)
				}
				if m1.Profile != nil {
					t.Errorf("Members[1].Profile: got %v, want nil（匿名）", m1.Profile)
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
				if resp.TotalMembers != 0 {
					t.Errorf("TotalMembers: got %d, want 0", resp.TotalMembers)
				}
			},
		},
		// 3. 正常: 匿名参加者のみ - 全員の profile が null
		{
			name:      "正常: 匿名参加者のみ - 全員の profile が null",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub: &stubEventJoinRepository{
				listMembers: []model.EventMember{
					{
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
				// 匿名参加は profile が null になる（profileId 削除後、匿名判定の唯一の手掛かり）。
				if resp.Members[0].Profile != nil {
					t.Errorf("Members[0].Profile: got %v, want nil（匿名）", resp.Members[0].Profile)
				}
				if resp.TotalCount != 1 {
					t.Errorf("TotalCount: got %d, want 1", resp.TotalCount)
				}
				if resp.TotalMembers != 1 {
					t.Errorf("TotalMembers: got %d, want 1", resp.TotalMembers)
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
		// 5. 異常: イベントが存在しない → ValidationError（兄弟エンドポイントと統一）
		{
			name:      "異常: イベントが存在しない → ValidationError（兄弟エンドポイントと統一）",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			joinStub:  &stubEventJoinRepository{listMembers: nil},
			eventStub: &stubEventRepository{
				ownerProfileIDErr: fmt.Errorf("event %s: %w", eventUID, repository.ErrEventNotFound),
			},
			wantValErr: true,
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
				if resp.TotalMembers != 0 {
					t.Errorf("TotalMembers: got %d, want 0", resp.TotalMembers)
				}
			},
			checkListMembersCalled: func(t *testing.T, stub *stubEventJoinRepository) {
				t.Helper()
				if stub.gotListMembersID != eventUID {
					t.Errorf("gotListMembersID: got %v, want %v", stub.gotListMembersID, eventUID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventJoinService(tt.joinStub, tt.eventStub, nil)

			resp, err := svc.ListMembers(context.Background(), tt.profileID, tt.eventID)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				return
			case tt.wantForbiddenErr:
				_ = assertForbiddenError(t, err)
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
				return
			}

			assertNoErr(t, err)

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
			if tt.checkListMembersCalled != nil {
				tt.checkListMembersCalled(t, tt.joinStub)
			}
		})
	}
}
