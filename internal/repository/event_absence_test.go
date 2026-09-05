package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// setTestEventDeadline はテスト用イベントの申込期限（application_deadline）を設定する。
// zero 値を渡すと NULL（期限なし）に戻す。
func setTestEventDeadline(t *testing.T, db *sql.DB, eventID uuid.UUID, deadline time.Time) {
	t.Helper()

	var d sql.NullTime
	if !deadline.IsZero() {
		d = sql.NullTime{Time: deadline, Valid: true}
	}
	const updateEvent = `
	UPDATE events
	SET application_deadline = $2
	WHERE id = $1
	`
	if _, err := db.ExecContext(context.Background(), updateEvent, eventID, d); err != nil {
		t.Fatalf("update test event deadline: %v", err)
	}
}

// setTestEventDates はテスト用イベントの開催日時（event_date・end_date）を更新する。
// end_date >= event_date の CHECK 制約を満たすため、両方を同時に設定する。
func setTestEventDates(t *testing.T, db *sql.DB, eventID uuid.UUID, eventDate, endDate time.Time) {
	t.Helper()

	const updateEvent = `
	UPDATE events
	SET event_date = $2, end_date = $3
	WHERE id = $1
	`
	if _, err := db.ExecContext(context.Background(), updateEvent, eventID, eventDate, endDate); err != nil {
		t.Fatalf("update test event dates: %v", err)
	}
}

// cancelTestEvent はテスト用イベントを取りやめ状態にする。
func cancelTestEvent(t *testing.T, db *sql.DB, eventID uuid.UUID) {
	t.Helper()

	const updateEvent = `
	UPDATE events
	SET cancelled_at = now()
	WHERE id = $1
	`
	if _, err := db.ExecContext(context.Background(), updateEvent, eventID); err != nil {
		t.Fatalf("cancel test event: %v", err)
	}
}

// joinTestMember はテスト用にログイン参加者を1人登録する（repo.Join 経由）。
func joinTestMember(t *testing.T, repo EventJoinRepository, eventID, profileID uuid.UUID) {
	t.Helper()

	member := &model.EventMember{
		EventID:     eventID,
		ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
		Username:    "参加者",
		MailAddress: uuid.NewString() + "@example.com",
		PartySize:   1,
		Categories: []model.MemberCategory{
			{Category: "大人", HeadCount: 1},
		},
	}
	if err := repo.Join(context.Background(), member); err != nil {
		t.Fatalf("Join() returned error: %v", err)
	}
}

// TestEventJoinPostgres_Absence は Absence が参加行削除・absence ログ追記・
// 主催者宛 outbox 予約を1トランザクションで行うこと、および各 sentinel エラーを
// 返すことを検証する。
func TestEventJoinPostgres_Absence(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	now := time.Now()

	// 期限なし・開催中のイベントにログイン参加者を1人作る。
	setupAbsentableEvent := func(t *testing.T) (eventID, profileID uuid.UUID) {
		t.Helper()
		eventID = insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		setTestEventDates(t, db, eventID, now.Add(-time.Hour), now.Add(time.Hour))
		profileID = insertTestProfile(t, db)
		joinTestMember(t, repo, eventID, profileID)
		return eventID, profileID
	}

	t.Run("正常: 参加行を削除し absence ログと organizer 宛 outbox を1トランザクションで記録する", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)

		createdAt, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "熱が出たため", "件名", "本文",
		)
		if err != nil {
			t.Fatalf("Absence() returned error: %v", err)
		}
		if createdAt.IsZero() {
			t.Error("Absence() returned zero createdAt, want non-zero")
		}

		// 参加行が削除されていること。
		var memberCount int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM event_members WHERE event_id = $1 AND profile_id = $2`, eventID, profileID,
		).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 0 {
			t.Errorf("member count = %d, want 0", memberCount)
		}

		// absence ログに reason・detail が記録されていること。
		var reason, detail sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT reason, detail FROM event_participation_logs WHERE event_id = $1 AND profile_id = $2 AND action = 'absence'`,
			eventID, profileID,
		).Scan(&reason, &detail); err != nil {
			t.Fatalf("select absence log: %v", err)
		}
		if !reason.Valid || reason.String != "illness" {
			t.Errorf("absence log reason = %v, want %q", reason, "illness")
		}
		if !detail.Valid || detail.String != "熱が出たため" {
			t.Errorf("absence log detail = %v, want %q", detail, "熱が出たため")
		}

		// 主催者宛（recipient_kind='organizer'）の outbox 行が1件予約されていること。
		var (
			recipientKind  string
			outboxSubject  string
			outboxBody     string
			outboxStatus   string
			outboxRowCount int
		)
		if err := db.QueryRowContext(context.Background(),
			`SELECT recipient_kind, subject, body, status, COUNT(*) OVER () FROM event_notification_outbox WHERE event_id = $1`,
			eventID,
		).Scan(&recipientKind, &outboxSubject, &outboxBody, &outboxStatus, &outboxRowCount); err != nil {
			t.Fatalf("query outbox row: %v", err)
		}
		if outboxRowCount != 1 {
			t.Errorf("outbox row count = %d, want 1", outboxRowCount)
		}
		if recipientKind != "organizer" {
			t.Errorf("outbox recipient_kind = %q, want %q", recipientKind, "organizer")
		}
		if outboxSubject != "件名" {
			t.Errorf("outbox subject = %q, want %q", outboxSubject, "件名")
		}
		if outboxBody != "本文" {
			t.Errorf("outbox body = %q, want %q", outboxBody, "本文")
		}
		if outboxStatus != "pending" {
			t.Errorf("outbox status = %q, want %q", outboxStatus, "pending")
		}
	})

	t.Run("正常: detail が空文字の場合は participation_logs の detail に NULL が保存される", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)

		if _, err := repo.Absence(
			context.Background(), eventID, profileID,
			"other", "", "件名", "本文",
		); err != nil {
			t.Fatalf("Absence() returned error: %v", err)
		}

		var detail sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT detail FROM event_participation_logs WHERE event_id = $1 AND profile_id = $2 AND action = 'absence'`,
			eventID, profileID,
		).Scan(&detail); err != nil {
			t.Fatalf("select absence log: %v", err)
		}
		if detail.Valid {
			t.Errorf("absence log detail = %q, want NULL", detail.String)
		}
	})

	t.Run("正常: reason が空文字の場合は participation_logs の reason に NULL が保存される", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)

		if _, err := repo.Absence(
			context.Background(), eventID, profileID,
			"", "熱が出たため", "件名", "本文",
		); err != nil {
			t.Fatalf("Absence() returned error: %v", err)
		}

		var reason sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT reason FROM event_participation_logs WHERE event_id = $1 AND profile_id = $2 AND action = 'absence'`,
			eventID, profileID,
		).Scan(&reason); err != nil {
			t.Fatalf("select absence log: %v", err)
		}
		if reason.Valid {
			t.Errorf("absence log reason = %q, want NULL", reason.String)
		}
	})

	t.Run("正常: 申込期限経過後のイベントは欠席連絡できる", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)
		setTestEventDeadline(t, db, eventID, now.Add(-time.Hour))

		if _, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "", "件名", "本文",
		); err != nil {
			t.Fatalf("Absence() returned error: %v", err)
		}
	})

	t.Run("異常: 申込期限前なら ErrAbsenceBeforeDeadline を返す", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)
		setTestEventDeadline(t, db, eventID, now.Add(time.Hour))

		_, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "", "件名", "本文",
		)
		if !errors.Is(err, ErrAbsenceBeforeDeadline) {
			t.Errorf("Absence() error = %v, want ErrAbsenceBeforeDeadline", err)
		}

		// 失敗したトランザクションはロールバックされるため、参加行とログが残ること。
		var memberCount int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM event_members WHERE event_id = $1 AND profile_id = $2`, eventID, profileID,
		).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 1 {
			t.Errorf("member count = %d, want 1（ロールバックされること）", memberCount)
		}
	})

	t.Run("異常: end_date 経過後なら ErrEventEnded を返す", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)
		setTestEventDates(t, db, eventID, now.Add(-2*time.Hour), now.Add(-time.Hour))

		_, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "", "件名", "本文",
		)
		if !errors.Is(err, ErrEventEnded) {
			t.Errorf("Absence() error = %v, want ErrEventEnded", err)
		}
	})

	t.Run("異常: 取りやめ済みイベントなら ErrEventCancelled を返す", func(t *testing.T) {
		eventID, profileID := setupAbsentableEvent(t)
		cancelTestEvent(t, db, eventID)

		_, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "", "件名", "本文",
		)
		if !errors.Is(err, ErrEventCancelled) {
			t.Errorf("Absence() error = %v, want ErrEventCancelled", err)
		}
	})

	t.Run("異常: 未参加なら ErrNotJoined を返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		setTestEventDates(t, db, eventID, now.Add(-time.Hour), now.Add(time.Hour))
		profileID := insertTestProfile(t, db)

		_, err := repo.Absence(
			context.Background(), eventID, profileID,
			"illness", "", "件名", "本文",
		)
		if !errors.Is(err, ErrNotJoined) {
			t.Errorf("Absence() error = %v, want ErrNotJoined", err)
		}
	})

	t.Run("異常: イベント不存在なら ErrEventNotFound を返す", func(t *testing.T) {
		profileID := insertTestProfile(t, db)

		_, err := repo.Absence(
			context.Background(), uuid.New(), profileID,
			"illness", "", "件名", "本文",
		)
		if !errors.Is(err, ErrEventNotFound) {
			t.Errorf("Absence() error = %v, want ErrEventNotFound", err)
		}
	})
}

// TestEventJoinPostgres_Leave_DeadlinePassed は Leave が申込期限ありイベントの
// 期限経過後の呼び出しを ErrDeadlinePassed で拒否すること、期限内・期限なしは
// 従来どおり受け付けることを検証する。
func TestEventJoinPostgres_Leave_DeadlinePassed(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	now := time.Now()

	// 期限ありイベントにログイン参加者を1人作る。
	setupDeadlineEvent := func(t *testing.T, deadline time.Time) (eventID, profileID uuid.UUID) {
		t.Helper()
		eventID = insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		setTestEventDates(t, db, eventID, now.Add(-time.Hour), now.Add(time.Hour))
		profileID = insertTestProfile(t, db)
		// 期限経過後は Join 自体が拒否されるため（ADR-0029）、申込を済ませてから期限を設定する。
		joinTestMember(t, repo, eventID, profileID)
		setTestEventDeadline(t, db, eventID, deadline)
		return eventID, profileID
	}

	t.Run("異常: 申込期限経過後のキャンセルは ErrDeadlinePassed で拒否される", func(t *testing.T) {
		eventID, profileID := setupDeadlineEvent(t, now.Add(-time.Hour))

		_, err := repo.Leave(context.Background(), eventID, profileID)
		if !errors.Is(err, ErrDeadlinePassed) {
			t.Errorf("Leave() error = %v, want ErrDeadlinePassed", err)
		}

		// 失敗したトランザクションはロールバックされるため、参加行が残ること。
		var memberCount int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM event_members WHERE event_id = $1 AND profile_id = $2`, eventID, profileID,
		).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 1 {
			t.Errorf("member count = %d, want 1（ロールバックされること）", memberCount)
		}
	})

	t.Run("正常: 申込期限内のキャンセルは受け付けられる", func(t *testing.T) {
		eventID, profileID := setupDeadlineEvent(t, now.Add(time.Hour))

		if _, err := repo.Leave(context.Background(), eventID, profileID); err != nil {
			t.Fatalf("Leave() returned error: %v", err)
		}
	})

	t.Run("正常: 期限なしイベントのキャンセルは受け付けられる", func(t *testing.T) {
		eventID, profileID := setupDeadlineEvent(t, time.Time{})

		if _, err := repo.Leave(context.Background(), eventID, profileID); err != nil {
			t.Fatalf("Leave() returned error: %v", err)
		}
	})
}

// TestEventNotificationOutboxPostgres_ListDue_ReturnsRecipientKind は outbox の
// recipient_kind が members（従来の CancelWithNotification 経由）・organizer（Absence 経由）
// のどちらの行でも ListDue で正しく返ることを検証する。
func TestEventNotificationOutboxPostgres_ListDue_ReturnsRecipientKind(t *testing.T) {
	db := requireTestDB(t)
	eventRepo := NewEventRepository(db)
	joinRepo := NewEventJoinRepository(db)
	outboxRepo := NewEventNotificationOutboxRepository(db)

	ownerID := insertTestProfile(t, db)
	now := time.Now()

	// members 宛行: 従来どおり CancelWithNotification が recipient_kind を指定せず
	// INSERT するため、DB の DEFAULT 'members' が入る。
	membersEventID := insertTestEvent(t, db, ownerID)
	if _, err := eventRepo.CancelWithNotification(context.Background(), membersEventID, "件名", "本文"); err != nil {
		t.Fatalf("CancelWithNotification() returned error: %v", err)
	}

	// organizer 宛行: Absence が recipient_kind='organizer' で INSERT する。
	organizerEventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, organizerEventID, "大人", 500)
	setTestEventDates(t, db, organizerEventID, now.Add(-time.Hour), now.Add(time.Hour))
	participantID := insertTestProfile(t, db)
	joinTestMember(t, joinRepo, organizerEventID, participantID)
	if _, err := joinRepo.Absence(
		context.Background(), organizerEventID, participantID,
		"illness", "", "件名", "本文",
	); err != nil {
		t.Fatalf("Absence() returned error: %v", err)
	}

	// ListDue は next_attempt_at <= 引数時刻 の行を返すため、未来の時刻を渡して
	// INSERT 済みの両行が確実に due になるようにする（INSERT の next_attempt_at は
	// DEFAULT now() のため、テスト開始時刻を渡すとミリ秒の揺らぎで漏れる）。
	due, err := outboxRepo.ListDue(context.Background(), now.Add(time.Minute), 100)
	if err != nil {
		t.Fatalf("ListDue() returned error: %v", err)
	}

	gotKinds := make(map[uuid.UUID]string, len(due))
	for _, item := range due {
		gotKinds[item.EventID] = item.RecipientKind
	}

	if got := gotKinds[membersEventID]; got != model.NotificationOutboxRecipientKindMembers {
		t.Errorf("members 宛行の recipient_kind = %q, want %q", got, model.NotificationOutboxRecipientKindMembers)
	}
	if got := gotKinds[organizerEventID]; got != model.NotificationOutboxRecipientKindOrganizer {
		t.Errorf("organizer 宛行の recipient_kind = %q, want %q", got, model.NotificationOutboxRecipientKindOrganizer)
	}
}

// TestEventPostgres_GetOrganizerEmail は GetOrganizerEmail が主催者のメールアドレスを
// 返すこと、イベント不存在・主催者プロフィール不在時に対応する sentinel エラーを
// 返すことを検証する。
func TestEventPostgres_GetOrganizerEmail(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	t.Run("正常: 主催者のメールアドレスを返す", func(t *testing.T) {
		ownerID := insertTestProfile(t, db)
		eventID := insertTestEvent(t, db, ownerID)

		got, err := repo.GetOrganizerEmail(context.Background(), eventID)
		if err != nil {
			t.Fatalf("GetOrganizerEmail() returned error: %v", err)
		}
		if got != ownerID.String()+"@example.com" {
			t.Errorf("GetOrganizerEmail() = %q, want %q", got, ownerID.String()+"@example.com")
		}
	})

	t.Run("異常: イベントが存在しない場合は ErrEventNotFound を返す", func(t *testing.T) {
		_, err := repo.GetOrganizerEmail(context.Background(), uuid.New())
		if !errors.Is(err, ErrEventNotFound) {
			t.Errorf("GetOrganizerEmail() error = %v, want ErrEventNotFound", err)
		}
	})
}
