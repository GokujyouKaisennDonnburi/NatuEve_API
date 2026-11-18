package repository

import (
	"context"
	"database/sql"
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
	if err := repo.Enqueue(ctx, "admin@example.com", subject, "デモ用の要望です", receivedAt); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var eventID sql.NullString
	var kind, recipient, body, status string
	var createdAt time.Time
	err := db.QueryRowContext(ctx, `SELECT id, event_id, recipient_kind, recipient_email, body, status, created_at
		FROM event_notification_outbox WHERE subject = $1`, subject).
		Scan(&id, &eventID, &kind, &recipient, &body, &status, &createdAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM event_notification_outbox WHERE id = $1", id); err != nil {
			t.Error(err)
		}
	})
	if eventID.Valid || kind != model.NotificationOutboxRecipientKindEmail || recipient != "admin@example.com" ||
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
	if item := claim.Item(); item.ID != id || item.EventID != uuid.Nil || item.RecipientEmail != recipient {
		t.Fatalf("unexpected claimed item: %+v", item)
	}
	if err := claim.MarkSent(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestContactOutboxRequiresEventForOtherKinds(t *testing.T) {
	db := requireTestDB(t)
	for _, kind := range []string{model.NotificationOutboxRecipientKindMembers, model.NotificationOutboxRecipientKindOrganizer} {
		t.Run(kind, func(t *testing.T) {
			_, err := db.Exec(`INSERT INTO event_notification_outbox(event_id, recipient_kind, subject, body)
				VALUES(NULL, $1, 'test', 'test')`, kind)
			if err == nil {
				t.Fatal("イベントに紐づく通知がevent_idなしで保存された")
			}
		})
	}
}
