package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

type stubContactRepo struct {
	calls             int
	to, subject, body string
	receivedAt        time.Time
	err               error
}

func (r *stubContactRepo) Enqueue(_ context.Context, to, subject, body string, receivedAt time.Time) error {
	r.calls++
	r.to, r.subject, r.body, r.receivedAt = to, subject, body, receivedAt
	return r.err
}

func TestContactServiceSubmit(t *testing.T) {
	tests := []struct {
		name           string
		category       model.ContactCategory
		message, label string
		invalid        bool
	}{
		{"要望", model.ContactCategoryRequest, " デモ用の要望です\n", "要望", false},
		{"不具合", model.ContactCategoryBug, "画面が開きません", "不具合報告", false},
		{"その他は本文省略可", model.ContactCategoryOther, "", "その他", false},
		{"空白本文", model.ContactCategoryRequest, "　\n\t", "要望", false},
		{"空カテゴリ", "", "", "", true},
		{"空白カテゴリ", " ", "", "", true},
		{"未定義カテゴリ", "support", "", "", true},
		{"カテゴリの大文字", "BUG", "", "", true},
		{"2000 runeは許可", model.ContactCategoryRequest, " \n" + strings.Repeat("🌱", 2000) + "　", "要望", false},
		{"2001 runeは拒否", model.ContactCategoryRequest, strings.Repeat("あ", 2001), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubContactRepo{}
			svc := NewContactService(repo, "admin@example.com")
			start := time.Now().UTC().Truncate(time.Microsecond)
			resp, err := svc.Submit(context.Background(), model.ContactRequest{Category: tt.category, Message: tt.message})
			if tt.invalid {
				var ve *ValidationError
				if !errors.As(err, &ve) || repo.calls != 0 {
					t.Fatalf("err=%v, enqueue calls=%d; want validation error and no enqueue", err, repo.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if resp.Category != tt.category || resp.Message != strings.TrimSpace(tt.message) {
				t.Fatalf("unexpected response: %+v", resp)
			}
			if resp.ReceivedAt.Before(start) || resp.ReceivedAt.After(time.Now()) || resp.ReceivedAt != repo.receivedAt {
				t.Errorf("receivedAt=%s, outbox time=%s", resp.ReceivedAt, repo.receivedAt)
			}
			if repo.calls != 1 || repo.to != "admin@example.com" || repo.subject != "【お問い合わせ】"+tt.label {
				t.Fatalf("unexpected enqueue: %+v", repo)
			}
			for _, text := range []string{"カテゴリ: " + tt.label, resp.Message, resp.ReceivedAt.Format(time.RFC3339Nano)} {
				if !strings.Contains(repo.body, text) {
					t.Errorf("mail body missing %q", text)
				}
			}
		})
	}
}

func TestContactServiceEnqueueFailure(t *testing.T) {
	cause := errors.New("database unavailable")
	svc := NewContactService(&stubContactRepo{err: cause}, "admin@example.com")
	resp, err := svc.Submit(context.Background(), model.ContactRequest{Category: model.ContactCategoryBug})
	if !errors.Is(err, cause) || !resp.ReceivedAt.IsZero() {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
}

func TestJoinEmailsContactFooter(t *testing.T) {
	for _, contactTo := range []string{"", "admin@example.com"} {
		t.Run(contactTo, func(t *testing.T) {
			_, confirmation := buildJoinConfirmationEmail("観察会", "https://web.example.com/confirm", contactTo)
			_, notice := buildAlreadyJoinedEmail("観察会", contactTo)
			for _, body := range []string{confirmation, notice} {
				if contactTo == "" {
					if strings.Contains(body, "お問い合わせ先:") {
						t.Fatal("未設定の窓口が表示された")
					}
				} else if !strings.HasSuffix(body, "\n\nお問い合わせ先: "+contactTo) {
					t.Fatalf("お問い合わせ先が末尾にない: %s", body)
				}
			}
		})
	}
}

func TestContactOutboxWorkerWithoutEvent(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "送信成功", true: "送信失敗を再試行"}[failure], func(t *testing.T) {
			item := model.EventNotificationOutbox{
				ID: uuid.New(), RecipientKind: model.NotificationOutboxRecipientKindEmail,
				RecipientEmail: "admin@example.com", Subject: "【お問い合わせ】要望", Body: "要望です",
			}
			repo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{item}}
			mailer := &workerFakeMailer{}
			if failure {
				mailer.sendErr = ErrMailRateLimited
			}
			// お問い合わせはイベント・参加者Repositoryに依存せず送信できる。
			worker := NewNotificationOutboxWorker(repo, nil, nil, mailer)
			worker.processOne(context.Background(), repo.claim(item))
			if mailer.callCount != 1 || len(mailer.gotEmails) != 1 || mailer.gotEmails[0].To != item.RecipientEmail ||
				mailer.gotEmails[0].Subject != item.Subject || mailer.gotEmails[0].Text != item.Body {
				t.Fatalf("unexpected send: %+v", mailer)
			}
			if failure {
				if len(repo.retryCalls) != 1 || len(repo.sentIDs) != 0 {
					t.Fatal("失敗が再試行に回らない")
				}
			} else if len(repo.sentIDs) != 1 {
				t.Fatal("送信済みに更新されない")
			}
		})
	}
}

func TestGuestJoinIncludesContactFooter(t *testing.T) {
	for _, outcome := range []repository.JoinOutcome{repository.JoinOutcomePending, repository.JoinOutcomeAlreadyJoined} {
		t.Run(string(outcome), func(t *testing.T) {
			repo := &stubEventJoinRepository{joinOutcome: outcome}
			svc := NewEventJoinService(repo, &stubEventRepository{title: "観察会"}, "https://web.example.com", "admin@example.com")
			_, err := svc.Join(context.Background(), uuid.New(), uuid.NullUUID{}, model.JoinEventRequest{
				Username: "デモ太郎", MailAddress: "guest@example.com",
				Participants: []model.ParticipantInput{{Category: "大人", HeadCount: 1}},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, body := range []string{repo.gotNotification.ConfirmationBody, repo.gotNotification.NoticeBody} {
				if !strings.HasSuffix(body, "\n\nお問い合わせ先: admin@example.com") {
					t.Fatal("窓口がメール予約へ渡されていない")
				}
			}
		})
	}
}
