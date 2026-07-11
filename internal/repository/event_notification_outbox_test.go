package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEventNotificationOutboxPostgres_ListDue_MarkSent_MarkRetry_MarkFailed は
// outbox リポジトリの CRUD 相当の各メソッドを一連の流れで検証する。
func TestEventNotificationOutboxPostgres_ListDue_MarkSent_MarkRetry_MarkFailed(t *testing.T) {
	db := requireTestDB(t)
	eventRepo := NewEventRepository(db)
	outboxRepo := NewEventNotificationOutboxRepository(db)

	profileID := insertTestProfile(t, db)

	newDueOutbox := func(t *testing.T, subject string) uuid.UUID {
		t.Helper()
		eventID := insertTestEvent(t, db, profileID)
		if _, err := eventRepo.CancelWithNotification(context.Background(), eventID, subject, "本文"); err != nil {
			t.Fatalf("CancelWithNotification() returned error: %v", err)
		}
		var outboxID uuid.UUID
		const query = `SELECT id FROM event_notification_outbox WHERE event_id = $1`
		if err := db.QueryRowContext(context.Background(), query, eventID).Scan(&outboxID); err != nil {
			t.Fatalf("query outbox id: %v", err)
		}
		return outboxID
	}

	t.Run("ListDue は next_attempt_at <= now かつ pending の行のみ返す", func(t *testing.T) {
		dueID := newDueOutbox(t, "due-subject")

		// 未来の next_attempt_at を持つ行は対象外になることを確認するため、
		// 別の行を作って next_attempt_at を未来に更新する。
		futureID := newDueOutbox(t, "future-subject")
		const updateFuture = `UPDATE event_notification_outbox SET next_attempt_at = $2 WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), updateFuture, futureID, time.Now().Add(1*time.Hour)); err != nil {
			t.Fatalf("update next_attempt_at: %v", err)
		}

		got, err := outboxRepo.ListDue(context.Background(), time.Now(), 100)
		if err != nil {
			t.Fatalf("ListDue() returned error: %v", err)
		}

		var foundDue, foundFuture bool
		for _, item := range got {
			if item.ID == dueID {
				foundDue = true
			}
			if item.ID == futureID {
				foundFuture = true
			}
		}
		if !foundDue {
			t.Error("ListDue() result does not contain the due outbox row")
		}
		if foundFuture {
			t.Error("ListDue() result contains a future-scheduled outbox row (should be excluded)")
		}
	})

	t.Run("MarkSent は status を sent にし sent_at を設定する", func(t *testing.T) {
		id := newDueOutbox(t, "sent-subject")

		if err := outboxRepo.MarkSent(context.Background(), id); err != nil {
			t.Fatalf("MarkSent() returned error: %v", err)
		}

		var status string
		var sentAtValid bool
		const query = `SELECT status, sent_at IS NOT NULL FROM event_notification_outbox WHERE id = $1`
		if err := db.QueryRowContext(context.Background(), query, id).Scan(&status, &sentAtValid); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if status != "sent" {
			t.Errorf("status = %q, want %q", status, "sent")
		}
		if !sentAtValid {
			t.Error("sent_at is NULL, want non-NULL")
		}

		// sent 状態は ListDue の対象外になること。
		due, err := outboxRepo.ListDue(context.Background(), time.Now(), 1000)
		if err != nil {
			t.Fatalf("ListDue() returned error: %v", err)
		}
		for _, item := range due {
			if item.ID == id {
				t.Error("sent 済みの行が ListDue() の結果に含まれている")
			}
		}
	})

	t.Run("MarkRetry は attempts をインクリメントし next_attempt_at/last_error を更新する", func(t *testing.T) {
		id := newDueOutbox(t, "retry-subject")
		nextAttemptAt := time.Now().Add(30 * time.Second).Truncate(time.Millisecond)

		if err := outboxRepo.MarkRetry(context.Background(), id, nextAttemptAt, "send failed: timeout"); err != nil {
			t.Fatalf("MarkRetry() returned error: %v", err)
		}

		var status string
		var attempts int
		var lastError string
		var gotNextAttemptAt time.Time
		const query = `SELECT status, attempts, last_error, next_attempt_at FROM event_notification_outbox WHERE id = $1`
		if err := db.QueryRowContext(context.Background(), query, id).Scan(&status, &attempts, &lastError, &gotNextAttemptAt); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if status != "pending" {
			t.Errorf("status = %q, want %q (retry はステータスを pending のまま維持する)", status, "pending")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
		if lastError != "send failed: timeout" {
			t.Errorf("last_error = %q, want %q", lastError, "send failed: timeout")
		}
		if !gotNextAttemptAt.Equal(nextAttemptAt) {
			t.Errorf("next_attempt_at = %v, want %v", gotNextAttemptAt, nextAttemptAt)
		}
	})

	t.Run("MarkFailed は status を failed にし attempts をインクリメントする", func(t *testing.T) {
		id := newDueOutbox(t, "failed-subject")

		if err := outboxRepo.MarkFailed(context.Background(), id, "send failed: permanent"); err != nil {
			t.Fatalf("MarkFailed() returned error: %v", err)
		}

		var status string
		var attempts int
		var lastError string
		const query = `SELECT status, attempts, last_error FROM event_notification_outbox WHERE id = $1`
		if err := db.QueryRowContext(context.Background(), query, id).Scan(&status, &attempts, &lastError); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if status != "failed" {
			t.Errorf("status = %q, want %q", status, "failed")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
		if lastError != "send failed: permanent" {
			t.Errorf("last_error = %q, want %q", lastError, "send failed: permanent")
		}

		// failed 状態は ListDue の対象外になること。
		due, err := outboxRepo.ListDue(context.Background(), time.Now(), 1000)
		if err != nil {
			t.Fatalf("ListDue() returned error: %v", err)
		}
		for _, item := range due {
			if item.ID == id {
				t.Error("failed 済みの行が ListDue() の結果に含まれている")
			}
		}
	})
}
