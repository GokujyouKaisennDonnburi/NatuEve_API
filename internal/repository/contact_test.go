package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

func TestContactPostgresEnqueueAndClaim(t *testing.T) {
	db := requireTestDB(t)
	repo := NewContactRepository(db)
	ctx := context.Background()
	subject := "【お問い合わせ】" + uuid.NewString()
	receivedAt := time.Now().UTC().Truncate(time.Microsecond)
	cleanupContactSubject(t, db, subject)
	if err := repo.Enqueue(ctx, "admin@example.com", subject, "デモ用の要望です", receivedAt, 24*time.Hour, 10); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var eventID sql.NullString
	var purpose, kind, recipient, body, status string
	var createdAt time.Time
	err := db.QueryRowContext(ctx, `SELECT id, event_id, purpose, recipient_kind, recipient_email, body, status, created_at
		FROM event_notification_outbox WHERE subject = $1`, subject).
		Scan(&id, &eventID, &purpose, &kind, &recipient, &body, &status, &createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if eventID.Valid || purpose != model.NotificationOutboxPurposeContact || kind != model.NotificationOutboxRecipientKindEmail || recipient != "admin@example.com" ||
		body != "デモ用の要望です" || status != model.NotificationOutboxStatusPending || !createdAt.Equal(receivedAt) {
		t.Fatalf("unexpected outbox row: event=%v kind=%s recipient=%s body=%s status=%s time=%s",
			eventID, kind, recipient, body, status, createdAt)
	}
	outbox := NewEventNotificationOutboxRepository(db)
	claim, err := outbox.ClaimNextDue(ctx, time.Now().Add(time.Second), pendingIDsExcept(t, db, id))
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil {
		t.Fatal("問い合わせをワーカーが取得できない")
	}
	defer func() { _ = claim.Release() }()
	if item := claim.Item(); item.ID != id || item.EventID.Valid || item.Purpose != model.NotificationOutboxPurposeContact || item.RecipientEmail != recipient {
		t.Fatalf("unexpected claimed item: %+v", item)
	}
	if err := claim.MarkSent(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestContactOutboxPurposeConstraints(t *testing.T) {
	db := requireTestDB(t)
	profileID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, profileID)
	for _, tt := range []struct {
		name, purpose, kind string
		eventID             uuid.NullUUID
		valid               bool
	}{
		{"問い合わせ", "contact", "email", uuid.NullUUID{}, true},
		{"イベント通知", "event", "email", uuid.NullUUID{UUID: eventID, Valid: true}, true},
		{"用途が不正", "unknown", "email", uuid.NullUUID{}, false},
		{"イベントメールにIDなし", "event", "email", uuid.NullUUID{}, false},
		{"参加者通知にIDなし", "event", "members", uuid.NullUUID{}, false},
		{"主催者通知にIDなし", "event", "organizer", uuid.NullUUID{}, false},
		{"問い合わせにイベントあり", "contact", "email", uuid.NullUUID{UUID: eventID, Valid: true}, false},
		{"問い合わせの宛先種別不正", "contact", "members", uuid.NullUUID{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback() })
			_, err = tx.ExecContext(context.Background(), `INSERT INTO event_notification_outbox
				(event_id, purpose, recipient_kind, recipient_email, subject, body)
				VALUES($1, $2, $3, 'admin@example.com', 'test', 'test')`, tt.eventID, tt.purpose, tt.kind)
			if (err == nil) != tt.valid {
				t.Fatalf("err=%v valid=%t", err, tt.valid)
			}
		})
	}
}

func cleanupContactSubject(t *testing.T, db *sql.DB, subject string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM event_notification_outbox WHERE subject = $1`, subject); err != nil {
			t.Error(err)
		}
	})
}

func TestContactPostgresAdmissionWindow(t *testing.T) {
	db := requireTestDB(t)
	repo := NewContactRepository(db)
	profileID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, profileID)
	for _, tt := range []struct {
		name, purpose, status string
		age                   time.Duration
		rejected              bool
	}{
		{"送信待ちも集計", "contact", "pending", time.Hour, true},
		{"送信済みも集計", "contact", "sent", time.Hour, true},
		{"失敗も集計", "contact", "failed", time.Hour, true},
		{"24時間ちょうどは期間外", "contact", "sent", 24 * time.Hour, false},
		{"24時間を過ぎた行は期間外", "contact", "sent", 25 * time.Hour, false},
		{"ゲスト確認メールは集計しない", "event", "pending", time.Hour, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Microsecond)
			subject := "contact-window-" + uuid.NewString()
			cleanupContactSubject(t, db, subject)
			id := uuid.NullUUID{UUID: eventID, Valid: tt.purpose == "event"}
			_, err := db.ExecContext(ctx, `INSERT INTO event_notification_outbox
				(event_id, purpose, recipient_kind, recipient_email, subject, body, status, created_at)
				VALUES($1, $2, 'email', 'first@example.com', $3, 'test', $4, $5)`, id, tt.purpose, subject, tt.status, now.Add(-tt.age))
			if err != nil {
				t.Fatal(err)
			}
			err = repo.Enqueue(ctx, "another@example.com", subject, "test", now, 24*time.Hour, 1)
			if tt.rejected {
				if !errors.Is(err, ErrContactLimitReached) {
					t.Fatalf("err=%v want limit reached", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM event_notification_outbox WHERE subject = $1`, subject).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 2
			if tt.rejected {
				want = 1
			}
			if count != want {
				t.Fatalf("rows=%d want=%d", count, want)
			}
		})
	}
}

func TestContactPostgresConcurrentLimit(t *testing.T) {
	db := requireTestDB(t)
	subject := "contact-concurrent-" + uuid.NewString()
	cleanupContactSubject(t, db, subject)
	const limit, requests = 10, 20
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Repositoryインスタンス・宛先が違っても、DBの共通枠を消費する。
			repo := NewContactRepository(db)
			results <- repo.Enqueue(context.Background(), fmt.Sprintf("admin%d@example.com", i), subject, "test", time.Now(), 24*time.Hour, limit)
		}()
	}
	wg.Wait()
	close(results)
	var success, rejected int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrContactLimitReached):
			rejected++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if success != limit || rejected != requests-limit {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
}

func TestContactPostgresFailedInsertDoesNotConsumeSlot(t *testing.T) {
	db := requireTestDB(t)
	repo := NewContactRepository(db)
	subject := "contact-rollback-" + uuid.NewString()
	cleanupContactSubject(t, db, subject)
	now := time.Now()
	cleanupContactSubject(t, db, strings.Repeat("a", 256))
	if err := repo.Enqueue(context.Background(), "admin@example.com", strings.Repeat("a", 256), "test", now, 24*time.Hour, 1); err == nil {
		t.Fatal("件名のDB制約違反が成功した")
	}
	if err := repo.Enqueue(context.Background(), "admin@example.com", subject, "test", now, 24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}
}
