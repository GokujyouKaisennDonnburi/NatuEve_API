package repository

import (
	"context"
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
