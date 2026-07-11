package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// countOutboxRowsForEvent はテスト用に event_notification_outbox の該当イベント行数を数える。
func countOutboxRowsForEvent(t *testing.T, db *sql.DB, eventID uuid.UUID) int {
	t.Helper()

	var count int
	const query = `SELECT COUNT(*) FROM event_notification_outbox WHERE event_id = $1`
	if err := db.QueryRowContext(context.Background(), query, eventID).Scan(&count); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	return count
}

// TestEventPostgres_CancelWithNotification は CancelWithNotification が
// イベントのキャンセル確定と outbox への通知予約を原子的に行うこと、
// 非冪等（2回目の呼び出しは ErrEventAlreadyCancelled）であること、
// イベント不存在時に ErrEventNotFound を返すことを検証する。
func TestEventPostgres_CancelWithNotification(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	t.Run("正常: キャンセル確定と outbox 予約が原子的に行われる", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		cancelledAt, err := repo.CancelWithNotification(context.Background(), eventID, "件名", "本文")
		if err != nil {
			t.Fatalf("CancelWithNotification() returned error: %v", err)
		}
		if cancelledAt.IsZero() {
			t.Error("CancelledAt is zero, want non-zero")
		}

		// events.cancelled_at が更新されていること。
		var dbCancelledAt sql.NullTime
		if err := db.QueryRowContext(context.Background(),
			`SELECT cancelled_at FROM events WHERE id = $1`, eventID,
		).Scan(&dbCancelledAt); err != nil {
			t.Fatalf("query cancelled_at: %v", err)
		}
		if !dbCancelledAt.Valid {
			t.Error("events.cancelled_at is NULL, want non-NULL")
		}

		// outbox に1件だけ pending で予約されていること。
		if got := countOutboxRowsForEvent(t, db, eventID); got != 1 {
			t.Fatalf("outbox rows = %d, want 1", got)
		}

		var subject, body, status string
		const outboxQuery = `SELECT subject, body, status FROM event_notification_outbox WHERE event_id = $1`
		if err := db.QueryRowContext(context.Background(), outboxQuery, eventID).Scan(&subject, &body, &status); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if subject != "件名" {
			t.Errorf("subject = %q, want %q", subject, "件名")
		}
		if body != "本文" {
			t.Errorf("body = %q, want %q", body, "本文")
		}
		if status != "pending" {
			t.Errorf("status = %q, want %q", status, "pending")
		}
	})

	t.Run("異常: 2回目の呼び出しは ErrEventAlreadyCancelled で outbox 行も増えない", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		if _, err := repo.CancelWithNotification(context.Background(), eventID, "件名1", "本文1"); err != nil {
			t.Fatalf("1回目の CancelWithNotification() returned error: %v", err)
		}
		if got := countOutboxRowsForEvent(t, db, eventID); got != 1 {
			t.Fatalf("1回目後の outbox rows = %d, want 1", got)
		}

		_, err := repo.CancelWithNotification(context.Background(), eventID, "件名2", "本文2")
		if !errors.Is(err, ErrEventAlreadyCancelled) {
			t.Fatalf("2回目の CancelWithNotification() error = %v, want ErrEventAlreadyCancelled", err)
		}

		// 2回目は失敗しているため outbox 行数は増えていないこと（原子性の確認）。
		if got := countOutboxRowsForEvent(t, db, eventID); got != 1 {
			t.Errorf("2回目後の outbox rows = %d, want 1 (増えてはいけない)", got)
		}
	})

	t.Run("異常: イベントが存在しない場合 ErrEventNotFound", func(t *testing.T) {
		_, err := repo.CancelWithNotification(context.Background(), uuid.New(), "件名", "本文")
		if !errors.Is(err, ErrEventNotFound) {
			t.Fatalf("CancelWithNotification() error = %v, want ErrEventNotFound", err)
		}
	})
}
