package repository

import (
	"context"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// TestReportPostgres_Create はレポート投稿時にイベントの更新日時も
// 更新されることを検証する。
func TestReportPostgres_Create(t *testing.T) {
	db := requireTestDB(t)
	repo := NewReportRepository(db)

	profileID := insertTestProfile(t, db)

	t.Run("正常: レポート投稿時にイベントのupdated_atが更新される", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		// 更新前の日時を過去に設定する。
		oldUpdatedAt := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

		if _, err := db.ExecContext(
			context.Background(),
			`UPDATE events SET updated_at = $2 WHERE id = $1`,
			eventID,
			oldUpdatedAt,
		); err != nil {
			t.Fatalf("set old updated_at: %v", err)
		}

		// レポート投稿
		report := &model.NewReport{
			EventID: eventID.String(),
			Content: "テストレポート",
		}

		resp, err := repo.Create(context.Background(), report)
		if err != nil {
			t.Fatalf("Create() returned error: %v", err)
		}

		if resp.ReportID == "" {
			t.Error("ReportID is empty, want non-empty")
		}

		// events.updated_at が更新されていることを確認する。
		var gotUpdatedAt time.Time
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT updated_at FROM events WHERE id = $1`,
			eventID,
		).Scan(&gotUpdatedAt); err != nil {
			t.Fatalf("query event updated_at: %v", err)
		}

		if !gotUpdatedAt.After(oldUpdatedAt) {
			t.Errorf(
				"events.updated_at = %v, want after %v",
				gotUpdatedAt,
				oldUpdatedAt,
			)
		}

		// レポート本体も登録されていることを確認する。
		var reportCount int
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM reports WHERE id = $1`,
			resp.ReportID,
		).Scan(&reportCount); err != nil {
			t.Fatalf("count reports: %v", err)
		}

		if reportCount != 1 {
			t.Errorf("report count = %d, want 1", reportCount)
		}
	})

	t.Run("異常: 存在しないイベントへのレポート投稿は失敗する", func(t *testing.T) {
		report := &model.NewReport{
			EventID: "00000000-0000-0000-0000-000000000000",
			Content: "存在しないイベントのレポート",
		}

		_, err := repo.Create(context.Background(), report)
		if err == nil {
			t.Fatal("Create() should return error for nonexistent event")
		}

		// レポートが登録されていないことを確認する。
		var reportCount int
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM reports WHERE event_id = $1`,
			report.EventID,
		).Scan(&reportCount); err != nil {
			t.Fatalf("count reports: %v", err)
		}

		if reportCount != 0 {
			t.Errorf("report count = %d, want 0", reportCount)
		}
	})
}
