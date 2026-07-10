package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// TestEventJoinPostgres_Join_WritesParticipationLog は Join が参加登録と同時に
// event_participation_logs へ join を追記すること、匿名参加ではログを残さないことを検証する。
func TestEventJoinPostgres_Join_WritesParticipationLog(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)

	tests := []struct {
		name          string
		loggedIn      bool
		wantLogCount  int
		wantLogAction string
	}{
		{name: "ログイン参加はjoinログを1件追記する", loggedIn: true, wantLogCount: 1, wantLogAction: "join"},
		{name: "匿名参加はログを追記しない", loggedIn: false, wantLogCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventID := insertTestEvent(t, db, ownerID)

			var profileID uuid.NullUUID
			if tt.loggedIn {
				profileID = uuid.NullUUID{UUID: insertTestProfile(t, db), Valid: true}
			}

			member := &model.EventMember{
				EventID:     eventID,
				ProfileID:   profileID,
				Username:    "参加者",
				MailAddress: uuid.NewString() + "@example.com",
				PartySize:   1,
			}

			if err := repo.Join(context.Background(), member); err != nil {
				t.Fatalf("Join() returned error: %v", err)
			}

			// 参加状態ログの件数と内容を検証する。
			const countQuery = `
			SELECT COUNT(*)
			FROM event_participation_logs
			WHERE event_id = $1
			`
			var got int
			if err := db.QueryRowContext(context.Background(), countQuery, eventID).Scan(&got); err != nil {
				t.Fatalf("count participation logs: %v", err)
			}
			if got != tt.wantLogCount {
				t.Fatalf("participation log count = %d, want %d", got, tt.wantLogCount)
			}

			if tt.wantLogCount > 0 {
				const selectQuery = `
				SELECT action, profile_id
				FROM event_participation_logs
				WHERE event_id = $1
				`
				var action string
				var loggedProfileID uuid.UUID
				if err := db.QueryRowContext(context.Background(), selectQuery, eventID).Scan(&action, &loggedProfileID); err != nil {
					t.Fatalf("select participation log: %v", err)
				}
				if action != tt.wantLogAction {
					t.Errorf("participation log action = %q, want %q", action, tt.wantLogAction)
				}
				if loggedProfileID != profileID.UUID {
					t.Errorf("participation log profile_id = %s, want %s", loggedProfileID, profileID.UUID)
				}
			}
		})
	}
}

// TestEventJoinPostgres_Leave_DeletesMemberAndWritesLog は Leave が参加行を削除し、
// 参加状態ログへ leave を追記すること、および未参加・イベント不存在時に
// 対応する sentinel エラーを返すことを検証する。
func TestEventJoinPostgres_Leave_DeletesMemberAndWritesLog(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)

	t.Run("正常: 参加行を削除し leave ログを1件追記する", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		profileID := insertTestProfile(t, db)

		// 事前にログイン参加させる（join ログが1件記録される）。
		member := &model.EventMember{
			EventID:     eventID,
			ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
			Username:    "参加者",
			MailAddress: uuid.NewString() + "@example.com",
			PartySize:   1,
		}
		if err := repo.Join(context.Background(), member); err != nil {
			t.Fatalf("Join() returned error: %v", err)
		}

		createdAt, err := repo.Leave(context.Background(), eventID, profileID)
		if err != nil {
			t.Fatalf("Leave() returned error: %v", err)
		}
		if createdAt.IsZero() {
			t.Error("Leave() returned zero createdAt, want non-zero")
		}

		// 参加行が削除されていることを確認する。
		const countMembers = `
		SELECT COUNT(*)
		FROM event_members
		WHERE event_id = $1 AND profile_id = $2
		`
		var memberCount int
		if err := db.QueryRowContext(context.Background(), countMembers, eventID, profileID).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 0 {
			t.Errorf("member count = %d, want 0", memberCount)
		}

		// leave ログが1件追記されていることを確認する（join と合わせて計2件）。
		const countLeaveLog = `
		SELECT COUNT(*)
		FROM event_participation_logs
		WHERE event_id = $1 AND profile_id = $2 AND action = 'leave'
		`
		var leaveCount int
		if err := db.QueryRowContext(context.Background(), countLeaveLog, eventID, profileID).Scan(&leaveCount); err != nil {
			t.Fatalf("count leave logs: %v", err)
		}
		if leaveCount != 1 {
			t.Errorf("leave log count = %d, want 1", leaveCount)
		}
	})

	t.Run("異常: 未参加なら ErrNotJoined を返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		profileID := insertTestProfile(t, db)

		_, err := repo.Leave(context.Background(), eventID, profileID)
		if !errors.Is(err, ErrNotJoined) {
			t.Errorf("Leave() error = %v, want ErrNotJoined", err)
		}
	})

	t.Run("異常: イベント不存在なら ErrEventNotFound を返す", func(t *testing.T) {
		profileID := insertTestProfile(t, db)

		_, err := repo.Leave(context.Background(), uuid.New(), profileID)
		if !errors.Is(err, ErrEventNotFound) {
			t.Errorf("Leave() error = %v, want ErrEventNotFound", err)
		}
	})
}
