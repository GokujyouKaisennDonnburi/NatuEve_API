package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// fakeMailer は Mailer のフェイク実装。テスト用。送信された emails を記録する。
type fakeMailer struct {
	sendErr error
	// SendBatch へ渡された引数を記録する。
	gotEmails []Email
}

func (f *fakeMailer) SendBatch(_ context.Context, emails []Email) error {
	f.gotEmails = emails
	return f.sendErr
}

// stubNotificationSendLogRepository は EventNotificationSendLogRepository のテスト用スタブ。
type stubNotificationSendLogRepository struct {
	// Reserve 返却値。reserveErr が nil のとき reserveID を返す。
	reserveID int64
	reserveErr error
	// Reserve 呼び出し時に渡された引数を記録する。
	gotReserveEventID uuid.UUID
	gotReserveNow     time.Time
	gotReserveWindow  time.Duration
	gotReserveLimit   int
	reserveCalls      int
	// MarkSent 返却値・引数記録。
	markSentErr  error
	gotMarkSentID int64
	gotMarkSentNow time.Time
	markSentCalls  int
	// Delete 返却値・引数記録。
	deleteErr   error
	gotDeleteID int64
	deleteCalls int
}

func (s *stubNotificationSendLogRepository) Reserve(
	_ context.Context,
	eventID uuid.UUID,
	now time.Time,
	window time.Duration,
	limit int,
) (int64, error) {
	s.gotReserveEventID = eventID
	s.gotReserveNow = now
	s.gotReserveWindow = window
	s.gotReserveLimit = limit
	s.reserveCalls++
	if s.reserveErr != nil {
		return 0, s.reserveErr
	}
	return s.reserveID, nil
}

func (s *stubNotificationSendLogRepository) MarkSent(_ context.Context, id int64, now time.Time) error {
	s.gotMarkSentID = id
	s.gotMarkSentNow = now
	s.markSentCalls++
	return s.markSentErr
}

func (s *stubNotificationSendLogRepository) Delete(_ context.Context, id int64) error {
	s.gotDeleteID = id
	s.deleteCalls++
	return s.deleteErr
}

// validSendEventNotificationRequest は正常系テスト用の最小限の有効なリクエスト。
func validSendEventNotificationRequest() model.SendEventNotificationRequest {
	return model.SendEventNotificationRequest{
		Subject: "【重要】明日のイベントは雨天決行です",
		Body:    "明日のイベントは予報通り雨となりますが、予定通り開催します。",
	}
}

func TestEventNotificationServiceSendBulk(t *testing.T) {
	const (
		eventID   = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
		ownerID   = "b2c3d4e5-f6a7-8901-bcde-f23456789012"
		otherUser = "c3d4e5f6-a7b8-9012-cdef-345678901234"
	)

	recipients := []model.EventRecipient{
		{MailAddress: "yamada@example.com"},
		{MailAddress: "sato@example.com"},
	}

	tests := []struct {
		name          string
		profileID     string
		req           model.SendEventNotificationRequest
		eventRepoStub *stubEventRepository
		joinRepoStub  *stubEventJoinRepository
		sendLogStub   *stubNotificationSendLogRepository
		mailer        *fakeMailer
		wantValErr    bool
		wantForbidden bool
		wantLimitErr  bool
		wantErr       bool
		checkResp     func(t *testing.T, resp model.SendEventNotificationResponse)
		checkMailer   func(t *testing.T, m *fakeMailer)
		checkSendLog  func(t *testing.T, s *stubNotificationSendLogRepository)
	}{
		{
			name:      "正常: 主催者が送信すると全参加者へ個別メールが送られる",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{
				recipients: recipients,
			},
			sendLogStub: &stubNotificationSendLogRepository{reserveID: 42},
			mailer:      &fakeMailer{},
			checkResp: func(t *testing.T, resp model.SendEventNotificationResponse) {
				t.Helper()
				if resp.EventID != eventID {
					t.Errorf("EventID: got %q, want %q", resp.EventID, eventID)
				}
				if resp.RecipientCount != 2 {
					t.Errorf("RecipientCount: got %d, want 2", resp.RecipientCount)
				}
				if resp.SentCount != 2 {
					t.Errorf("SentCount: got %d, want 2", resp.SentCount)
				}
				if resp.FailedCount != 0 {
					t.Errorf("FailedCount: got %d, want 0", resp.FailedCount)
				}
			},
			checkMailer: func(t *testing.T, m *fakeMailer) {
				t.Helper()
				if len(m.gotEmails) != 2 {
					t.Fatalf("送信された Email 数: got %d, want 2", len(m.gotEmails))
				}
				for i, e := range m.gotEmails {
					// ADR-0004: To は単一宛先であり、複数アドレスが結合されていないこと。
					if e.To != recipients[i].MailAddress {
						t.Errorf("Email[%d].To: got %q, want %q", i, e.To, recipients[i].MailAddress)
					}
					if e.Subject != "【重要】明日のイベントは雨天決行です" {
						t.Errorf("Email[%d].Subject: got %q", i, e.Subject)
					}
				}
			},
			checkSendLog: func(t *testing.T, s *stubNotificationSendLogRepository) {
				t.Helper()
				if s.reserveCalls != 1 {
					t.Fatalf("Reserve の呼び出し回数: got %d, want 1", s.reserveCalls)
				}
				if s.gotReserveWindow != notificationSendWindow {
					t.Errorf("Reserve への window: got %v, want %v", s.gotReserveWindow, notificationSendWindow)
				}
				if s.gotReserveLimit != maxNotificationSendsPerEvent {
					t.Errorf("Reserve への limit: got %d, want %d", s.gotReserveLimit, maxNotificationSendsPerEvent)
				}
				wantEventID, _ := uuid.Parse(eventID)
				if s.gotReserveEventID != wantEventID {
					t.Errorf("Reserve への eventID: got %v, want %v", s.gotReserveEventID, wantEventID)
				}
				if s.markSentCalls != 1 {
					t.Fatalf("MarkSent の呼び出し回数: got %d, want 1", s.markSentCalls)
				}
				if s.gotMarkSentID != 42 {
					t.Errorf("MarkSent への予約 ID: got %d, want 42", s.gotMarkSentID)
				}
				if s.deleteCalls != 0 {
					t.Errorf("予約の取り消しは行われないはず: got %d 回", s.deleteCalls)
				}
			},
		},
		{
			name:      "異常: 主催者以外が送信すると ForbiddenError",
			profileID: otherUser,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{recipients: recipients},
			sendLogStub:  &stubNotificationSendLogRepository{},
			mailer:       &fakeMailer{},
			wantForbidden: true,
		},
		{
			name:      "異常: イベントが存在しない場合 ValidationError",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileIDErr: fmt.Errorf("event xxx: %w", repository.ErrEventNotFound),
			},
			joinRepoStub: &stubEventJoinRepository{recipients: recipients},
			sendLogStub:  &stubNotificationSendLogRepository{},
			mailer:       &fakeMailer{},
			wantValErr:   true,
		},
		{
			name:      "異常: subject が空の場合 ValidationError",
			profileID: ownerID,
			req: model.SendEventNotificationRequest{
				Subject: "",
				Body:    "本文",
			},
			eventRepoStub: &stubEventRepository{ownerProfileID: ownerID},
			joinRepoStub:  &stubEventJoinRepository{recipients: recipients},
			sendLogStub:   &stubNotificationSendLogRepository{},
			mailer:        &fakeMailer{},
			wantValErr:    true,
		},
		{
			name:      "異常: subject が空白のみの場合 ValidationError",
			profileID: ownerID,
			req: model.SendEventNotificationRequest{
				Subject: "   ",
				Body:    "本文",
			},
			eventRepoStub: &stubEventRepository{ownerProfileID: ownerID},
			joinRepoStub:  &stubEventJoinRepository{recipients: recipients},
			sendLogStub:   &stubNotificationSendLogRepository{},
			mailer:        &fakeMailer{},
			wantValErr:    true,
		},
		{
			name:      "異常: body が空の場合 ValidationError",
			profileID: ownerID,
			req: model.SendEventNotificationRequest{
				Subject: "件名",
				Body:    "",
			},
			eventRepoStub: &stubEventRepository{ownerProfileID: ownerID},
			joinRepoStub:  &stubEventJoinRepository{recipients: recipients},
			sendLogStub:   &stubNotificationSendLogRepository{},
			mailer:        &fakeMailer{},
			wantValErr:    true,
		},
		{
			name:      "異常: 送信先の参加者が0件の場合 ValidationError",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{recipients: nil},
			sendLogStub:  &stubNotificationSendLogRepository{},
			mailer:       &fakeMailer{},
			wantValErr:   true,
		},
		{
			name:      "異常: mailer.SendBatch が失敗した場合エラー伝播",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{recipients: recipients},
			sendLogStub:  &stubNotificationSendLogRepository{reserveID: 42},
			mailer:       &fakeMailer{sendErr: errors.New("send failed")},
			wantErr:      true,
			checkSendLog: func(t *testing.T, s *stubNotificationSendLogRepository) {
				t.Helper()
				if s.deleteCalls != 1 {
					t.Fatalf("予約の取り消し回数: got %d, want 1", s.deleteCalls)
				}
				if s.gotDeleteID != 42 {
					t.Errorf("取り消された予約 ID: got %d, want 42", s.gotDeleteID)
				}
				if s.markSentCalls != 0 {
					t.Errorf("送信済み記録は呼ばれないはず: got %d 回", s.markSentCalls)
				}
			},
		},
		{
			name:      "異常: 送信回数上限に達している場合、送信せず拒否される",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{recipients: recipients},
			sendLogStub: &stubNotificationSendLogRepository{
				reserveErr: repository.ErrNotificationSendLimitReached,
			},
			mailer:       &fakeMailer{},
			wantLimitErr: true,
			checkSendLog: func(t *testing.T, s *stubNotificationSendLogRepository) {
				t.Helper()
				if s.reserveCalls != 1 {
					t.Fatalf("Reserve の呼び出し回数: got %d, want 1", s.reserveCalls)
				}
				if s.markSentCalls != 0 {
					t.Errorf("送信済み記録は呼ばれないはず: got %d 回", s.markSentCalls)
				}
				if s.deleteCalls != 0 {
					t.Errorf("枠の返却は行われないはず: got %d 回", s.deleteCalls)
				}
			},
			checkMailer: func(t *testing.T, m *fakeMailer) {
				t.Helper()
				if len(m.gotEmails) != 0 {
					t.Errorf("メールが送信されてはならない: got %d 通", len(m.gotEmails))
				}
			},
		},
		{
			name:      "異常: 予約の確保が DB エラーで失敗した場合エラー伝播",
			profileID: ownerID,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub: &stubEventJoinRepository{recipients: recipients},
			sendLogStub:  &stubNotificationSendLogRepository{reserveErr: errors.New("db error")},
			mailer:       &fakeMailer{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventNotificationService(tt.eventRepoStub, tt.joinRepoStub, tt.sendLogStub, tt.mailer)

			resp, err := svc.SendBulk(context.Background(), tt.profileID, eventID, tt.req)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				assertReserveNotCalled(t, tt.sendLogStub)
				return
			case tt.wantForbidden:
				_ = assertForbiddenError(t, err)
				assertReserveNotCalled(t, tt.sendLogStub)
				return
			case tt.wantLimitErr:
				if !errors.Is(err, ErrNotificationSendLimitExceeded) {
					t.Fatalf("ErrNotificationSendLimitExceeded を期待したが: %v", err)
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
			if tt.checkMailer != nil {
				tt.checkMailer(t, tt.mailer)
			}
			if tt.checkSendLog != nil {
				tt.checkSendLog(t, tt.sendLogStub)
			}
		})
	}
}

// assertReserveNotCalled は送信ログリポジトリの Reserve が呼ばれていないことを確認する。
// 検証・認可エラーで処理が中断されたケースで、枠を消費していないことを保証する。
func assertReserveNotCalled(t *testing.T, s *stubNotificationSendLogRepository) {
	t.Helper()
	if s.reserveCalls != 0 {
		t.Errorf("Reserve が呼ばれた: %d 回", s.reserveCalls)
	}
}

// 送信先が上限件数を超える場合は ValidationError になることを確認する。
func TestEventNotificationServiceSendBulk_TooManyRecipients(t *testing.T) {
	const eventID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	const ownerID = "b2c3d4e5-f6a7-8901-bcde-f23456789012"

	recipients := make([]model.EventRecipient, maxNotificationRecipients+1)
	for i := range recipients {
		recipients[i] = model.EventRecipient{
			MailAddress: fmt.Sprintf("user%d@example.com", i),
		}
	}

	eventRepoStub := &stubEventRepository{ownerProfileID: ownerID}
	joinRepoStub := &stubEventJoinRepository{recipients: recipients}
	sendLogStub := &stubNotificationSendLogRepository{}
	mailer := &fakeMailer{}

	svc := NewEventNotificationService(eventRepoStub, joinRepoStub, sendLogStub, mailer)

	_, err := svc.SendBulk(context.Background(), ownerID, eventID, validSendEventNotificationRequest())
	_ = assertValidationError(t, err)
}
