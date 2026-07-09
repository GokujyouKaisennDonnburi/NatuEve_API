package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	appdb "github.com/GokujyouKaisennDonnburi/NatuEve_API/db"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// testDB は DATABASE_URL が設定されている場合のみ生成される共有 *sql.DB。
// 未設定の環境ではコンパイル確認のみ行い、DB 依存テストはスキップする。
var testDB *sql.DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	sqlDB, err := appdb.Open(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open test db: %v\n", err)
		os.Exit(1)
	}
	if err := appdb.Migrate(ctx, sqlDB); err != nil {
		fmt.Fprintf(os.Stderr, "failed to migrate test db: %v\n", err)
		os.Exit(1)
	}
	testDB = sqlDB

	code := m.Run()
	_ = sqlDB.Close()
	os.Exit(code)
}

// requireTestDB は DB 接続が利用可能な場合のみ *sql.DB を返す。
// DATABASE_URL 未設定の環境ではテストをスキップする。
func requireTestDB(t *testing.T) *sql.DB {
	t.Helper()

	if testDB == nil {
		t.Skip("DATABASE_URL not set; skipping DB-dependent test")
	}
	return testDB
}

// insertTestProfile はテスト用の profiles 行を1件作成する。
func insertTestProfile(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertProfile = `
	INSERT INTO profiles(id, email)
	VALUES($1, $2)
	`
	if _, err := db.ExecContext(context.Background(), insertProfile, id, fmt.Sprintf("%s@example.com", id)); err != nil {
		t.Fatalf("insert test profile: %v", err)
	}
	return id
}

// insertTestEvent はテスト用の events 行を1件作成する。
func insertTestEvent(t *testing.T, db *sql.DB, profileID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertEvent = `
	INSERT INTO events(id, profile_id, title, event_date)
	VALUES($1, $2, $3, $4)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertEvent,
		id,
		profileID,
		"テストイベント",
		time.Now(),
	); err != nil {
		t.Fatalf("insert test event: %v", err)
	}
	return id
}

// TestEventParticipationLogPostgres_Create は Create の正常系(join/leave両方)と
// イベント不存在時に ErrEventNotFound を返すことを検証する。
func TestEventParticipationLogPostgres_Create(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventParticipationLogRepository(db)

	profileID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, profileID)

	tests := []struct {
		name   string
		action string
	}{
		{name: "joinログを追記できる", action: "join"},
		{name: "leaveログを追記できる", action: "leave"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &model.EventParticipationLog{
				EventID:   eventID,
				ProfileID: profileID,
				Action:    tt.action,
			}

			if err := repo.Create(context.Background(), log); err != nil {
				t.Fatalf("Create() returned error: %v", err)
			}

			if log.ID == uuid.Nil {
				t.Error("Create() did not populate log.ID")
			}
			if log.CreatedAt.IsZero() {
				t.Error("Create() did not populate log.CreatedAt")
			}
		})
	}
}

// TestEventParticipationLogPostgres_Create_EventNotFound は
// 存在しないイベントIDを指定した場合に ErrEventNotFound が返ることを検証する。
func TestEventParticipationLogPostgres_Create_EventNotFound(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventParticipationLogRepository(db)

	profileID := insertTestProfile(t, db)

	log := &model.EventParticipationLog{
		EventID:   uuid.New(),
		ProfileID: profileID,
		Action:    "join",
	}

	err := repo.Create(context.Background(), log)
	if !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("Create() error = %v, want ErrEventNotFound", err)
	}
}
