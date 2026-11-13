package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEventNotificationSendLogPostgres_Reserve は Reserve の上限判定・ウィンドウ判定と
// MarkSent / Delete の動作を検証する。
func TestEventNotificationSendLogPostgres_Reserve(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventNotificationSendLogRepository(db)

	profileID := insertTestProfile(t, db)
	newEvent := func(t *testing.T) uuid.UUID {
		t.Helper()
		eventID := insertTestEvent(t, db, profileID)
		t.Cleanup(func() {
			const cleanupQuery = `DELETE FROM event_notification_send_log WHERE event_id = $1`
			if _, err := db.ExecContext(context.Background(), cleanupQuery, eventID); err != nil {
				t.Errorf("cleanup send log: %v", err)
			}
		})
		return eventID
	}

	ctx := context.Background()
	now := time.Now()

	t.Run("上限未満の間は枠を確保でき、上限到達後は拒否される", func(t *testing.T) {
		eventID := newEvent(t)

		first, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 2)
		if err != nil {
			t.Fatalf("Reserve() 1st returned error: %v", err)
		}
		second, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 2)
		if err != nil {
			t.Fatalf("Reserve() 2nd returned error: %v", err)
		}
		if first == second {
			t.Errorf("予約 ID が重複している: id=%d", first)
		}

		if _, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 2); !errors.Is(err, ErrNotificationSendLimitReached) {
			t.Errorf("Reserve() 3rd error = %v, want ErrNotificationSendLimitReached", err)
		}
	})

	t.Run("ウィンドウ外の予約は数えない", func(t *testing.T) {
		eventID := newEvent(t)

		// 25 時間前の予約を 2 件入れる（直接 INSERT）。
		const insertOld = `
			INSERT INTO event_notification_send_log (event_id, reserved_at)
			VALUES ($1, $2)`
		old := now.Add(-25 * time.Hour)
		for range 2 {
			if _, err := db.ExecContext(ctx, insertOld, eventID, old); err != nil {
				t.Fatalf("insert old send log: %v", err)
			}
		}

		// 上限 2 だがウィンドウ外は数えないため、2 回確保できる。
		for i := range 2 {
			if _, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 2); err != nil {
				t.Fatalf("Reserve() %dth returned error: %v", i+1, err)
			}
		}
		if _, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 2); !errors.Is(err, ErrNotificationSendLimitReached) {
			t.Errorf("Reserve() error = %v, want ErrNotificationSendLimitReached", err)
		}
	})

	t.Run("MarkSent は予約行に送信日時を記録する", func(t *testing.T) {
		eventID := newEvent(t)

		id, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 1)
		if err != nil {
			t.Fatalf("Reserve() returned error: %v", err)
		}

		sentAt := now.Add(time.Minute)
		if err := repo.MarkSent(ctx, id, sentAt); err != nil {
			t.Fatalf("MarkSent() returned error: %v", err)
		}

		var gotSentAt *time.Time
		const query = `SELECT sent_at FROM event_notification_send_log WHERE id = $1`
		if err := db.QueryRowContext(ctx, query, id).Scan(&gotSentAt); err != nil {
			t.Fatalf("query sent_at: %v", err)
		}
		if gotSentAt == nil {
			t.Fatal("sent_at が記録されていない")
		}
	})

	t.Run("Delete は予約行を削除して枠を返す", func(t *testing.T) {
		eventID := newEvent(t)

		id, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 1)
		if err != nil {
			t.Fatalf("Reserve() returned error: %v", err)
		}
		if err := repo.Delete(ctx, id); err != nil {
			t.Fatalf("Delete() returned error: %v", err)
		}

		// 削除済みのため、上限 1 で再び確保できる。
		if _, err := repo.Reserve(ctx, eventID, now, 24*time.Hour, 1); err != nil {
			t.Fatalf("Reserve() after Delete returned error: %v", err)
		}
	})
}

// TestEventNotificationSendLogPostgres_ReserveConcurrent は同一イベントへの同時予約が
// 上限件数で直列化されることを検証する。複数インスタンス（複数 goroutine）から同時に
// Reserve しても、成功は limit 件に限られる（ADR-0046）。
func TestEventNotificationSendLogPostgres_ReserveConcurrent(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventNotificationSendLogRepository(db)

	profileID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, profileID)
	t.Cleanup(func() {
		const cleanupQuery = `DELETE FROM event_notification_send_log WHERE event_id = $1`
		if _, err := db.ExecContext(context.Background(), cleanupQuery, eventID); err != nil {
			t.Errorf("cleanup send log: %v", err)
		}
	})

	const (
		limit      = 2
		goroutines = 6
		window     = 24 * time.Hour
	)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		rejected  int
		otherErrs []error
	)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Reserve(context.Background(), eventID, time.Now(), window, limit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrNotificationSendLimitReached):
				rejected++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	wg.Wait()

	if len(otherErrs) != 0 {
		t.Fatalf("予期しないエラー: %v", otherErrs)
	}
	if successes != limit {
		t.Errorf("成功した予約数: got %d, want %d", successes, limit)
	}
	if rejected != goroutines-limit {
		t.Errorf("拒否された予約数: got %d, want %d", rejected, goroutines-limit)
	}

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM event_notification_send_log WHERE event_id = $1`, eventID).Scan(&count); err != nil {
		t.Fatalf("count send log: %v", err)
	}
	if count != limit {
		t.Errorf("記録された行数: got %d, want %d", count, limit)
	}
}
