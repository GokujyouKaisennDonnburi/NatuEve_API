package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
		mailer        *fakeMailer
		wantValErr    bool
		wantForbidden bool
		wantErr       bool
		checkResp     func(t *testing.T, resp model.SendEventNotificationResponse)
		checkMailer   func(t *testing.T, m *fakeMailer)
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
			mailer: &fakeMailer{},
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
		},
		{
			name:      "異常: 主催者以外が送信すると ForbiddenError",
			profileID: otherUser,
			req:       validSendEventNotificationRequest(),
			eventRepoStub: &stubEventRepository{
				ownerProfileID: ownerID,
			},
			joinRepoStub:  &stubEventJoinRepository{recipients: recipients},
			mailer:        &fakeMailer{},
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
			mailer:       &fakeMailer{sendErr: errors.New("send failed")},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventNotificationService(tt.eventRepoStub, tt.joinRepoStub, tt.mailer)

			resp, err := svc.SendBulk(context.Background(), tt.profileID, eventID, tt.req)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				return
			case tt.wantForbidden:
				_ = assertForbiddenError(t, err)
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
		})
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
	mailer := &fakeMailer{}

	svc := NewEventNotificationService(eventRepoStub, joinRepoStub, mailer)

	_, err := svc.SendBulk(context.Background(), ownerID, eventID, validSendEventNotificationRequest())
	_ = assertValidationError(t, err)
}
