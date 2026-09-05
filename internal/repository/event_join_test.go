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
			insertTestCost(t, db, eventID, "大人", 500)

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
				Categories: []model.MemberCategory{
					{Category: "大人", HeadCount: 1},
				},
			}

			if err := repo.Join(context.Background(), member); err != nil {
				t.Fatalf("Join() returned error: %v", err)
			}

			// ログの内訳はログ本体と同じ条件で残る（匿名参加はログごと残らない）。
			const countLogCategories = `
			SELECT COUNT(*)
			FROM event_participation_log_categories
			WHERE event_id = $1
			`
			var gotLogCategories int
			if err := db.QueryRowContext(context.Background(), countLogCategories, eventID).Scan(&gotLogCategories); err != nil {
				t.Fatalf("count participation log categories: %v", err)
			}
			if gotLogCategories != tt.wantLogCount {
				t.Errorf("participation log category count = %d, want %d", gotLogCategories, tt.wantLogCount)
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
		insertTestCost(t, db, eventID, "大人", 500)
		profileID := insertTestProfile(t, db)

		// 事前にログイン参加させる（join ログが1件記録される）。
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

		// 申込の内訳は参加行と一緒に CASCADE 削除される。
		const countMemberCategories = `
		SELECT COUNT(*)
		FROM event_member_categories
		WHERE event_id = $1
		`
		var memberCategoryCount int
		if err := db.QueryRowContext(context.Background(), countMemberCategories, eventID).Scan(&memberCategoryCount); err != nil {
			t.Fatalf("count member categories: %v", err)
		}
		if memberCategoryCount != 0 {
			t.Errorf("member category count = %d, want 0", memberCategoryCount)
		}

		// join 時に記録したログの内訳は履歴として残る。
		const countLogCategories = `
		SELECT COUNT(*)
		FROM event_participation_log_categories
		WHERE event_id = $1
		`
		var logCategoryCount int
		if err := db.QueryRowContext(context.Background(), countLogCategories, eventID).Scan(&logCategoryCount); err != nil {
			t.Fatalf("count participation log categories: %v", err)
		}
		if logCategoryCount != 1 {
			t.Errorf("participation log category count = %d, want 1", logCategoryCount)
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

// TestEventJoinPostgres_GetMemberByProfile は GetMemberByProfile が
// (event_id, profile_id) に一致する申込1件をカテゴリ内訳（昇順）付きで返すこと、
// 未申込・イベント不存在・匿名申込では対応する sentinel エラーを返すことを検証する。
func TestEventJoinPostgres_GetMemberByProfile(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)

	t.Run("正常: 内訳2件の申込をカテゴリ名昇順で取得できる", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		insertTestCost(t, db, eventID, "学生", 300)
		profileID := insertTestProfile(t, db)

		member := &model.EventMember{
			EventID:     eventID,
			ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
			Username:    "山田太郎",
			MailAddress: uuid.NewString() + "@example.com",
			PartySize:   3,
			Categories: []model.MemberCategory{
				{Category: "学生", HeadCount: 1},
				{Category: "大人", HeadCount: 2},
			},
		}
		if err := repo.Join(context.Background(), member); err != nil {
			t.Fatalf("Join() returned error: %v", err)
		}

		got, err := repo.GetMemberByProfile(context.Background(), eventID, profileID)
		if err != nil {
			t.Fatalf("GetMemberByProfile() returned error: %v", err)
		}
		if got.Username != "山田太郎" {
			t.Errorf("Username: got %q, want %q", got.Username, "山田太郎")
		}
		if got.MailAddress != member.MailAddress {
			t.Errorf("MailAddress: got %q, want %q", got.MailAddress, member.MailAddress)
		}
		if got.PartySize != 3 {
			t.Errorf("PartySize: got %d, want 3", got.PartySize)
		}
		if !got.CreatedAt.Equal(member.CreatedAt) {
			t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, member.CreatedAt)
		}
		wantCategories := []model.MemberCategory{
			{Category: "大人", HeadCount: 2},
			{Category: "学生", HeadCount: 1},
		}
		if len(got.Categories) != len(wantCategories) {
			t.Fatalf("Categories: got %d件, want %d件", len(got.Categories), len(wantCategories))
		}
		for i, want := range wantCategories {
			if got.Categories[i].Category != want.Category || got.Categories[i].HeadCount != want.HeadCount {
				t.Errorf(
					"Categories[%d]: got {%q %d}, want {%q %d}",
					i, got.Categories[i].Category, got.Categories[i].HeadCount, want.Category, want.HeadCount,
				)
			}
		}
	})

	t.Run("正常: 同一イベントに複数人の申込があっても自分の行だけを返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		insertTestCost(t, db, eventID, "学生", 300)

		// join は同一イベントへの申込を1件作る。2人の人数・カテゴリを変えておくことで、
		// 行を取り違えた場合に値の比較で必ず落ちるようにする。
		join := func(t *testing.T, profileID uuid.UUID, username string, category model.MemberCategory) *model.EventMember {
			t.Helper()
			member := &model.EventMember{
				EventID:     eventID,
				ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
				Username:    username,
				MailAddress: uuid.NewString() + "@example.com",
				PartySize:   category.HeadCount,
				Categories:  []model.MemberCategory{category},
			}
			if err := repo.Join(context.Background(), member); err != nil {
				t.Fatalf("Join() returned error: %v", err)
			}
			return member
		}

		meID := insertTestProfile(t, db)
		otherID := insertTestProfile(t, db)
		me := join(t, meID, "自分", model.MemberCategory{Category: "大人", HeadCount: 2})
		other := join(t, otherID, "他人", model.MemberCategory{Category: "学生", HeadCount: 4})

		got, err := repo.GetMemberByProfile(context.Background(), eventID, meID)
		if err != nil {
			t.Fatalf("GetMemberByProfile() returned error: %v", err)
		}
		if got.Username != "自分" || got.MailAddress != me.MailAddress {
			t.Errorf(
				"他人の申込が返っている: got (%q, %q), want (%q, %q)",
				got.Username, got.MailAddress, "自分", me.MailAddress,
			)
		}
		if got.PartySize != 2 {
			t.Errorf("PartySize: got %d, want 2（他人の人数が混ざっていないこと）", got.PartySize)
		}
		if len(got.Categories) != 1 || got.Categories[0].Category != "大人" || got.Categories[0].HeadCount != 2 {
			t.Errorf("Categories: got %+v, want [{大人 2}]（他人の内訳が混ざっていないこと）", got.Categories)
		}

		// profileID を変えれば同じイベントでもう一方の申込が返る（WHERE 句が profile_id で
		// 絞れていることの裏返しの確認）。
		gotOther, err := repo.GetMemberByProfile(context.Background(), eventID, otherID)
		if err != nil {
			t.Fatalf("GetMemberByProfile() returned error: %v", err)
		}
		if gotOther.Username != "他人" || gotOther.MailAddress != other.MailAddress || gotOther.PartySize != 4 {
			t.Errorf(
				"gotOther = (%q, %q, %d), want (%q, %q, 4)",
				gotOther.Username, gotOther.MailAddress, gotOther.PartySize, "他人", other.MailAddress,
			)
		}
	})

	t.Run("正常: 内訳を持たない申込は Categories が空スライスになる", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		profileID := insertTestProfile(t, db)
		insertTestEventMemberWithProfile(t, db, eventID, profileID, uuid.NewString()+"@example.com", 1)

		got, err := repo.GetMemberByProfile(context.Background(), eventID, profileID)
		if err != nil {
			t.Fatalf("GetMemberByProfile() returned error: %v", err)
		}
		if got.Categories == nil {
			t.Error("Categories = nil, want empty slice")
		}
		if len(got.Categories) != 0 {
			t.Errorf("Categories = %#v, want empty", got.Categories)
		}
	})

	t.Run("異常: 未申込なら ErrNotJoined を返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		profileID := insertTestProfile(t, db)

		_, err := repo.GetMemberByProfile(context.Background(), eventID, profileID)
		if !errors.Is(err, ErrNotJoined) {
			t.Errorf("GetMemberByProfile() error = %v, want ErrNotJoined", err)
		}
	})

	t.Run("異常: イベント不存在なら ErrEventNotFound を返す", func(t *testing.T) {
		profileID := insertTestProfile(t, db)

		_, err := repo.GetMemberByProfile(context.Background(), uuid.New(), profileID)
		if !errors.Is(err, ErrEventNotFound) {
			t.Errorf("GetMemberByProfile() error = %v, want ErrEventNotFound", err)
		}
	})

	t.Run("異常: 匿名申込の行は別profileIDで引いてもヒットせず ErrNotJoined", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestEventMember(t, db, eventID, uuid.NewString()+"@example.com", 1)
		profileID := insertTestProfile(t, db)

		_, err := repo.GetMemberByProfile(context.Background(), eventID, profileID)
		if !errors.Is(err, ErrNotJoined) {
			t.Errorf("GetMemberByProfile() error = %v, want ErrNotJoined", err)
		}
	})
}

// updateTestProfileDetails はテスト用の profiles 行の display_name・avatar_url を更新する。
// insertTestProfile はメールアドレスのみ設定するため、プロフィールサマリーの検証に
// display_name・avatar_url が必要なテストではこのヘルパーで追加設定する。
func updateTestProfileDetails(t *testing.T, db *sql.DB, profileID uuid.UUID, displayName, avatarURL string) {
	t.Helper()

	const updateProfile = `
	UPDATE profiles
	SET display_name = $2, avatar_url = $3
	WHERE id = $1
	`
	if _, err := db.ExecContext(context.Background(), updateProfile, profileID, displayName, avatarURL); err != nil {
		t.Fatalf("update test profile details: %v", err)
	}
}

// TestEventJoinPostgres_ListMembers_ReturnsProfileSummary は ListMembers が
// created_at 昇順で参加者を返すこと、ログイン参加者は profiles から LEFT JOIN した
// プロフィールサマリー（display_name・avatar_url）を保持すること、匿名参加者は
// Profile が nil になることを検証する。
func TestEventJoinPostgres_ListMembers_ReturnsProfileSummary(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)
	insertTestCost(t, db, eventID, "学生", 300)

	// ログイン参加者用プロフィール（表示名・アイコン URL を設定する）。
	loggedInProfileID := insertTestProfile(t, db)
	updateTestProfileDetails(t, db, loggedInProfileID, "なちゅいべ太郎", "https://example.com/avatar.png")

	// ログイン参加者を登録する（内訳は2カテゴリ）。
	loggedInMember := &model.EventMember{
		EventID:     eventID,
		ProfileID:   uuid.NullUUID{UUID: loggedInProfileID, Valid: true},
		Username:    "山田太郎",
		MailAddress: uuid.NewString() + "@example.com",
		PartySize:   3,
		Categories: []model.MemberCategory{
			{Category: "大人", HeadCount: 2},
			{Category: "学生", HeadCount: 1},
		},
	}
	if err := repo.Join(context.Background(), loggedInMember); err != nil {
		t.Fatalf("Join() (logged-in) returned error: %v", err)
	}

	// 匿名参加者を登録する（profile_id は NULL）。
	anonymousMember := &model.EventMember{
		EventID:     eventID,
		ProfileID:   uuid.NullUUID{},
		Username:    "匿名花子",
		MailAddress: uuid.NewString() + "@example.com",
		PartySize:   1,
		Categories: []model.MemberCategory{
			{Category: "大人", HeadCount: 1},
		},
	}
	if err := repo.Join(context.Background(), anonymousMember); err != nil {
		t.Fatalf("Join() (anonymous) returned error: %v", err)
	}

	members, err := repo.ListMembers(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListMembers() returned error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(members))
	}

	// created_at 昇順で返ること。
	if members[0].CreatedAt.After(members[1].CreatedAt) {
		t.Errorf(
			"members not sorted by created_at ascending: members[0]=%v, members[1]=%v",
			members[0].CreatedAt, members[1].CreatedAt,
		)
	}

	// 検証対象は添字ではなく Username で特定する。ORDER BY m.created_at には二次キーが無く、
	// 同一マイクロ秒で created_at がタイになると返却順が不定になるため、
	// 「1件目がログイン参加者」を前提にすると理論上テストが取り違える。
	byUsername := make(map[string]model.EventMember, len(members))
	for _, m := range members {
		byUsername[m.Username] = m
	}

	tests := []struct {
		name           string
		username       string
		wantProfile    bool
		wantCategories []model.MemberCategory
	}{
		{
			name:        "ログイン参加は Profile が非 nil で display_name・avatar_url を保持する",
			username:    "山田太郎",
			wantProfile: true,
			wantCategories: []model.MemberCategory{
				{Category: "大人", HeadCount: 2},
				{Category: "学生", HeadCount: 1},
			},
		},
		{
			name:        "匿名参加は Profile が nil",
			username:    "匿名花子",
			wantProfile: false,
			wantCategories: []model.MemberCategory{
				{Category: "大人", HeadCount: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member, ok := byUsername[tt.username]
			if !ok {
				t.Fatalf("参加者 %q が ListMembers の結果に含まれていない", tt.username)
			}

			// 内訳を検証する。並び順は DB の照合順に左右されるため、カテゴリ名で引いて比較する。
			gotCategories := make(map[string]int, len(member.Categories))
			for _, c := range member.Categories {
				gotCategories[c.Category] = c.HeadCount
			}
			if len(gotCategories) != len(tt.wantCategories) {
				t.Errorf("Categories: got %d件, want %d件", len(gotCategories), len(tt.wantCategories))
			}
			for _, want := range tt.wantCategories {
				if got := gotCategories[want.Category]; got != want.HeadCount {
					t.Errorf("Categories[%q]: got %d, want %d", want.Category, got, want.HeadCount)
				}
			}

			if !tt.wantProfile {
				if member.Profile != nil {
					t.Errorf("Profile: got %v, want nil（匿名）", member.Profile)
				}
				return
			}

			if member.Profile == nil {
				t.Fatal("Profile: got nil, want non-nil")
			}
			if member.Profile.ID != loggedInProfileID.String() {
				t.Errorf("Profile.ID: got %q, want %q", member.Profile.ID, loggedInProfileID.String())
			}
			if member.Profile.DisplayName != "なちゅいべ太郎" {
				t.Errorf("Profile.DisplayName: got %q, want %q", member.Profile.DisplayName, "なちゅいべ太郎")
			}
			if member.Profile.AvatarURL != "https://example.com/avatar.png" {
				t.Errorf("Profile.AvatarURL: got %q, want %q", member.Profile.AvatarURL, "https://example.com/avatar.png")
			}
		})
	}
}

// TestEventJoinPostgres_Join_ResolvesCategories は Join がカテゴリ名を event_costs へ解決して
// 内訳を保存すること、解決できない・重複するカテゴリを sentinel エラーで弾くことを検証する。
func TestEventJoinPostgres_Join_ResolvesCategories(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)

	// newMember は検証用の申込を組み立てる。
	newMember := func(eventID uuid.UUID, categories ...model.MemberCategory) *model.EventMember {
		partySize := 0
		for _, c := range categories {
			partySize += c.HeadCount
		}
		return &model.EventMember{
			EventID:     eventID,
			Username:    "参加者",
			MailAddress: uuid.NewString() + "@example.com",
			PartySize:   partySize,
			Categories:  categories,
		}
	}

	// countMembers は指定イベントの参加行数を返す（エラー時のロールバック確認に使う）。
	countMembers := func(t *testing.T, eventID uuid.UUID) int {
		t.Helper()
		const query = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
		var n int
		if err := db.QueryRowContext(context.Background(), query, eventID).Scan(&n); err != nil {
			t.Fatalf("count members: %v", err)
		}
		return n
	}

	t.Run("正常: 大文字小文字が違っても解決し、event_costs 側の表記に正規化する", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		adultID := insertTestCost(t, db, eventID, "Adult", 500)

		member := newMember(eventID, model.MemberCategory{Category: "adult", HeadCount: 2})
		if err := repo.Join(context.Background(), member); err != nil {
			t.Fatalf("Join() returned error: %v", err)
		}

		if member.Categories[0].CostID != adultID {
			t.Errorf("CostID: got %v, want %v", member.Categories[0].CostID, adultID)
		}
		if member.Categories[0].Category != "Adult" {
			t.Errorf("Category: got %q, want %q（event_costs 側の表記に揃える）", member.Categories[0].Category, "Adult")
		}
		if member.ID == uuid.Nil {
			t.Error("member.ID が埋められていない")
		}

		const selectCategory = `
		SELECT cost_id, head_count
		FROM event_member_categories
		WHERE member_id = $1
		`
		var (
			gotCostID    uuid.UUID
			gotHeadCount int
		)
		if err := db.QueryRowContext(context.Background(), selectCategory, member.ID).Scan(&gotCostID, &gotHeadCount); err != nil {
			t.Fatalf("select member category: %v", err)
		}
		if gotCostID != adultID || gotHeadCount != 2 {
			t.Errorf("保存された内訳 = (%v, %d), want (%v, 2)", gotCostID, gotHeadCount, adultID)
		}
	})

	t.Run("異常: イベントに存在しないカテゴリは ErrCategoryNotFound", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)

		member := newMember(eventID, model.MemberCategory{Category: "学生", HeadCount: 1})
		err := repo.Join(context.Background(), member)
		if !errors.Is(err, ErrCategoryNotFound) {
			t.Fatalf("Join() error = %v, want ErrCategoryNotFound", err)
		}
		if n := countMembers(t, eventID); n != 0 {
			t.Errorf("参加行が %d 件残っている, want 0（トランザクションがロールバックされること）", n)
		}
	})

	t.Run("異常: 他イベントのカテゴリ名は解決できない", func(t *testing.T) {
		otherEventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, otherEventID, "大人", 500)

		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "学生", 300)

		member := newMember(eventID, model.MemberCategory{Category: "大人", HeadCount: 1})
		if err := repo.Join(context.Background(), member); !errors.Is(err, ErrCategoryNotFound) {
			t.Fatalf("Join() error = %v, want ErrCategoryNotFound", err)
		}
		if n := countMembers(t, eventID); n != 0 {
			t.Errorf("参加行が %d 件残っている, want 0", n)
		}
	})

	t.Run("異常: 表記違いで同一カテゴリを重複指定すると ErrDuplicateCategory", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "Adult", 500)

		member := newMember(
			eventID,
			model.MemberCategory{Category: "Adult", HeadCount: 1},
			model.MemberCategory{Category: "adult", HeadCount: 2},
		)
		if err := repo.Join(context.Background(), member); !errors.Is(err, ErrDuplicateCategory) {
			t.Fatalf("Join() error = %v, want ErrDuplicateCategory", err)
		}
		if n := countMembers(t, eventID); n != 0 {
			t.Errorf("参加行が %d 件残っている, want 0", n)
		}
	})
}

// insertTestEventMemberWithProfile はテスト用の event_members 行を1件、profile_id 付きで
// 内訳なしで作成する。insertTestEventMember（internal/repository/event_test.go）は
// profile_id を NULL にするため、ログイン参加者の申込を GetMemberByProfile で
// 引けるようにするにはこちらを使う。
func insertTestEventMemberWithProfile(
	t *testing.T,
	db *sql.DB,
	eventID, profileID uuid.UUID,
	mailAddress string,
	partySize int,
) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertMember = `
	INSERT INTO event_members(id, event_id, profile_id, username, mail_address, party_size)
	VALUES($1, $2, $3, $4, $5, $6)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertMember,
		id,
		eventID,
		profileID,
		"テスト参加者",
		mailAddress,
		partySize,
	); err != nil {
		t.Fatalf("insert test event member with profile: %v", err)
	}
	return id
}

// TestEventJoinPostgres_Join_ApplicationDeadline は申込期限の判定を検証する（ADR-0029）。
// 期限経過後の申込は拒否し、期限前・期限なしは受け付ける。
func TestEventJoinPostgres_Join_ApplicationDeadline(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	now := time.Now()

	// setupEvent は費用カテゴリ「大人」を持つイベントを、指定の申込期限で作成する。
	setupEvent := func(t *testing.T, deadline sql.NullTime) uuid.UUID {
		t.Helper()
		eventID := insertTestEventWithApplicationDeadline(t, db, ownerID, deadline)
		insertTestCost(t, db, eventID, "大人", 500)
		return eventID
	}

	// countMembers は指定イベントの参加行数を返す。
	countMembers := func(t *testing.T, eventID uuid.UUID) int {
		t.Helper()
		const query = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
		var n int
		if err := db.QueryRowContext(context.Background(), query, eventID).Scan(&n); err != nil {
			t.Fatalf("count members: %v", err)
		}
		return n
	}

	tests := []struct {
		name     string
		deadline sql.NullTime
		wantErr  error
	}{
		{
			name:     "正常: 申込期限前は申し込める",
			deadline: sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		},
		{
			name:     "正常: 申込期限なし(NULL)は常時申し込める",
			deadline: sql.NullTime{},
		},
		{
			name:     "異常: 申込期限経過後は ErrDeadlinePassed を返す",
			deadline: sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
			wantErr:  ErrDeadlinePassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventID := setupEvent(t, tt.deadline)
			member := &model.EventMember{
				EventID:     eventID,
				Username:    "参加者",
				MailAddress: uuid.NewString() + "@example.com",
				PartySize:   1,
				Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
			}

			err := repo.Join(context.Background(), member)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Join() error = %v, want %v", err, tt.wantErr)
				}
				// 期限切れの申込は参加行を残さない。
				if n := countMembers(t, eventID); n != 0 {
					t.Errorf("member count = %d, want 0", n)
				}
				return
			}

			if err != nil {
				t.Fatalf("Join() returned error: %v", err)
			}
			if n := countMembers(t, eventID); n != 1 {
				t.Errorf("member count = %d, want 1", n)
			}
		})
	}
}
