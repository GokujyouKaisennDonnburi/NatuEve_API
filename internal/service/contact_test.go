package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

type stubContactRepo struct {
	calls             int
	to, subject, body string
	receivedAt        time.Time
	window            time.Duration
	limit             int
	err               error
}

func (r *stubContactRepo) Enqueue(_ context.Context, to, subject, body string, receivedAt time.Time, window time.Duration, limit int) error {
	r.calls++
	r.window, r.limit = window, limit
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
			if repo.window != 24*time.Hour || repo.limit != 10 {
				t.Fatalf("window=%s limit=%d", repo.window, repo.limit)
			}
			if repo.calls != 1 || repo.to != "admin@example.com" || repo.subject != "【お問い合わせ】"+tt.label {
				t.Fatalf("unexpected enqueue: %+v", repo)
			}
			for _, text := range []string{"カテゴリ: " + tt.label, resp.Message, resp.ReceivedAt.In(time.FixedZone("JST", 9*60*60)).Format("2006-01-02 15:04:05 MST")} {
				if !strings.Contains(repo.body, text) {
					t.Errorf("mail body missing %q", text)
				}
			}
		})
	}
}

func TestContactServiceEnqueueFailure(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cause error
	}{
		{"保存失敗", errors.New("database unavailable")},
		{"全体上限超過", repository.ErrContactLimitReached},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactService(&stubContactRepo{err: tt.cause}, "admin@example.com")
			resp, err := svc.Submit(context.Background(), model.ContactRequest{Category: model.ContactCategoryBug})
			if !errors.Is(err, tt.cause) || !resp.ReceivedAt.IsZero() {
				t.Fatalf("resp=%+v err=%v", resp, err)
			}
		})
	}
}
