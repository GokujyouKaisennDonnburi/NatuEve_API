package repository

import (
	"context"
	"database/sql"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// claimAllDue は now 時点で処理対象の行を 1 件ずつ確保して集める。確保した行はその場で解放し、
// 除外リストで次へ進む。DB には他のテストの行も残りうるため、結果から自分の行を探して使う。
func claimAllDue(t *testing.T, repo EventNotificationOutboxRepository, now time.Time) []model.EventNotificationOutbox {
	t.Helper()

	var (
		items   []model.EventNotificationOutbox
		exclude []uuid.UUID
	)
	for {
		claim, err := repo.ClaimNextDue(context.Background(), now, exclude)
		if err != nil {
			t.Fatalf("ClaimNextDue() returned error: %v", err)
		}
		if claim == nil {
			return items
		}
		items = append(items, claim.Item())
		exclude = append(exclude, claim.Item().ID)
		if err := claim.Release(); err != nil {
			t.Fatalf("Release() returned error: %v", err)
		}
	}
}

// pendingIDsExcept は pending の outbox 行の ID のうち、keep 以外を返す。
// 対象の行だけを確保させたいテストで、excludeIDs に渡す。
func pendingIDsExcept(t *testing.T, db *sql.DB, keep ...uuid.UUID) []uuid.UUID {
	t.Helper()

	const query = `SELECT id FROM event_notification_outbox WHERE status = 'pending'`
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("query pending outbox: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan pending outbox: %v", err)
		}
		if !slices.Contains(keep, id) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pending outbox: %v", err)
	}
	return ids
}

// TestEventNotificationOutboxPostgres_Claim は ClaimNextDue と、返された OutboxClaim の
// Mark* / Release を一連の流れで検証する。
func TestEventNotificationOutboxPostgres_Claim(t *testing.T) {
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

	// claimByID は指定した行を確保するまで ClaimNextDue を繰り返す（他テストの行は解放する）。
	claimByID := func(t *testing.T, id uuid.UUID) OutboxClaim {
		t.Helper()
		var exclude []uuid.UUID
		for {
			claim, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), exclude)
			if err != nil {
				t.Fatalf("ClaimNextDue() returned error: %v", err)
			}
			if claim == nil {
				t.Fatalf("outbox %v を確保できなかった", id)
			}
			if claim.Item().ID == id {
				return claim
			}
			exclude = append(exclude, claim.Item().ID)
			_ = claim.Release()
		}
	}

	t.Run("ClaimNextDue は next_attempt_at <= now かつ pending の行のみ返す", func(t *testing.T) {
		dueID := newDueOutbox(t, "due-subject")

		// 未来の next_attempt_at を持つ行は対象外になることを確認するため、
		// 別の行を作って next_attempt_at を未来に更新する。
		futureID := newDueOutbox(t, "future-subject")
		const updateFuture = `UPDATE event_notification_outbox SET next_attempt_at = $2 WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), updateFuture, futureID, time.Now().Add(1*time.Hour)); err != nil {
			t.Fatalf("update next_attempt_at: %v", err)
		}

		var foundDue, foundFuture bool
		for _, item := range claimAllDue(t, outboxRepo, time.Now().Add(time.Minute)) {
			if item.ID == dueID {
				foundDue = true
			}
			if item.ID == futureID {
				foundFuture = true
			}
		}
		if !foundDue {
			t.Error("ClaimNextDue() の結果に処理対象の行が含まれない")
		}
		if foundFuture {
			t.Error("ClaimNextDue() の結果に未来の next_attempt_at の行が含まれている（除外されるべき）")
		}
	})

	t.Run("Release は行を変更せず、再度確保できる", func(t *testing.T) {
		id := newDueOutbox(t, "release-subject")

		claim := claimByID(t, id)
		if err := claim.Release(); err != nil {
			t.Fatalf("Release() returned error: %v", err)
		}

		var status string
		var attempts int
		const query = `SELECT status, attempts FROM event_notification_outbox WHERE id = $1`
		if err := db.QueryRowContext(context.Background(), query, id).Scan(&status, &attempts); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if status != "pending" || attempts != 0 {
			t.Errorf("status = %q, attempts = %d, want pending / 0", status, attempts)
		}

		again := claimByID(t, id)
		_ = again.Release()
	})

	t.Run("excludeIDs", func(t *testing.T) {
		// 対象の行を最も古い next_attempt_at にし、除外が効いていなければ必ず確保される状態にする。
		id := newDueOutbox(t, "exclude-subject")
		const updateOldest = `UPDATE event_notification_outbox SET next_attempt_at = '2000-01-01' WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), updateOldest, id); err != nil {
			t.Fatalf("update next_attempt_at: %v", err)
		}

		tests := []struct {
			name       string
			excludeIDs []uuid.UUID
			wantClaim  bool
		}{
			{name: "nil なら最も古い行を確保する", excludeIDs: nil, wantClaim: true},
			{name: "空スライスなら最も古い行を確保する", excludeIDs: []uuid.UUID{}, wantClaim: true},
			{name: "含めた行は確保されない", excludeIDs: []uuid.UUID{id}, wantClaim: false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				claim, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), tt.excludeIDs)
				if err != nil {
					t.Fatalf("ClaimNextDue() returned error: %v", err)
				}
				got := claim != nil && claim.Item().ID == id
				if claim != nil {
					_ = claim.Release()
				}
				if got != tt.wantClaim {
					t.Errorf("対象の行を確保した = %v, want %v", got, tt.wantClaim)
				}
			})
		}
	})

	t.Run("MarkSent は status を sent にし sent_at を設定する", func(t *testing.T) {
		id := newDueOutbox(t, "sent-subject")

		claim := claimByID(t, id)
		if err := claim.MarkSent(context.Background()); err != nil {
			t.Fatalf("MarkSent() returned error: %v", err)
		}
		_ = claim.Release() // 確定後の Release は何もしない

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

		// sent 状態は確保の対象外になること。
		for _, item := range claimAllDue(t, outboxRepo, time.Now().Add(time.Minute)) {
			if item.ID == id {
				t.Error("sent 済みの行が ClaimNextDue() の結果に含まれている")
			}
		}
	})

	t.Run("MarkRetry は attempts をインクリメントし next_attempt_at/last_error を更新する", func(t *testing.T) {
		id := newDueOutbox(t, "retry-subject")
		nextAttemptAt := time.Now().Add(30 * time.Second).Truncate(time.Millisecond)

		claim := claimByID(t, id)
		if err := claim.MarkRetry(context.Background(), nextAttemptAt, "send failed: timeout"); err != nil {
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

		claim := claimByID(t, id)
		if err := claim.MarkFailed(context.Background(), "send failed: permanent"); err != nil {
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

		// failed 状態は確保の対象外になること。
		for _, item := range claimAllDue(t, outboxRepo, time.Now().Add(time.Minute)) {
			if item.ID == id {
				t.Error("failed 済みの行が ClaimNextDue() の結果に含まれている")
			}
		}
	})
}

// TestEventNotificationOutboxPostgres_Claim_Exclusive は、複数の処理が同時に走っても
// 同じ outbox 行を二重に確保しないこと（FOR UPDATE SKIP LOCKED）を検証する。
func TestEventNotificationOutboxPostgres_Claim_Exclusive(t *testing.T) {
	db := requireTestDB(t)
	eventRepo := NewEventRepository(db)
	outboxRepo := NewEventNotificationOutboxRepository(db)

	profileID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, profileID)
	if _, err := eventRepo.CancelWithNotification(context.Background(), eventID, "件名", "本文"); err != nil {
		t.Fatalf("CancelWithNotification() returned error: %v", err)
	}
	var wantID uuid.UUID
	const query = `SELECT id FROM event_notification_outbox WHERE event_id = $1`
	if err := db.QueryRowContext(context.Background(), query, eventID).Scan(&wantID); err != nil {
		t.Fatalf("query outbox id: %v", err)
	}

	// 他テストの pending 行が混ざらないよう、対象の行以外は excludeIDs で除外する。
	others := pendingIDsExcept(t, db, wantID)

	t.Run("確保中の行は別の処理から見えない", func(t *testing.T) {
		first, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), others)
		if err != nil || first == nil || first.Item().ID != wantID {
			t.Fatalf("1 つ目の ClaimNextDue() = (%v, %v), want 対象の行", first, err)
		}
		defer func() { _ = first.Release() }()

		second, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), others)
		if err != nil {
			t.Fatalf("2 つ目の ClaimNextDue() returned error: %v", err)
		}
		if second != nil {
			_ = second.Release()
			t.Fatal("確保中の行を別の処理も確保できた（二重処理になる）")
		}
	})

	t.Run("同時に確保を試みても 1 つの処理だけが行を得る", func(t *testing.T) {
		const workers = 8
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			claimed int
			claims  []OutboxClaim
		)
		start := make(chan struct{})
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				claim, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), others)
				if err != nil {
					t.Errorf("ClaimNextDue() returned error: %v", err)
					return
				}
				if claim != nil {
					mu.Lock()
					claimed++
					claims = append(claims, claim)
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		for _, c := range claims {
			_ = c.Release()
		}

		if claimed != 1 {
			t.Errorf("同時に確保できた処理の数 = %d, want 1", claimed)
		}
	})

	t.Run("ロック中の行は飛ばされ、ロックされていない別の行が確保される", func(t *testing.T) {
		otherEventID := insertTestEvent(t, db, profileID)
		if _, err := eventRepo.CancelWithNotification(context.Background(), otherEventID, "件名2", "本文2"); err != nil {
			t.Fatalf("CancelWithNotification() returned error: %v", err)
		}
		var otherID uuid.UUID
		if err := db.QueryRowContext(context.Background(), query, otherEventID).Scan(&otherID); err != nil {
			t.Fatalf("query outbox id: %v", err)
		}

		first, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), others)
		if err != nil || first == nil {
			t.Fatalf("1 つ目の ClaimNextDue() = (%v, %v), want 行", first, err)
		}
		defer func() { _ = first.Release() }()

		second, err := outboxRepo.ClaimNextDue(context.Background(), time.Now().Add(time.Minute), others)
		if err != nil || second == nil {
			t.Fatalf("2 つ目の ClaimNextDue() = (%v, %v), want 別の行", second, err)
		}
		defer func() { _ = second.Release() }()

		if second.Item().ID == first.Item().ID {
			t.Error("ロック中の行が 2 つ目にも返った")
		}
		got := map[uuid.UUID]bool{first.Item().ID: true, second.Item().ID: true}
		if !got[wantID] || !got[otherID] {
			t.Errorf("確保した行 = %v, want {%v, %v}", got, wantID, otherID)
		}
	})
}
