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

// joinConfirmedTestMember はテスト用の申込を confirmed で登録する。
func joinConfirmedTestMember(t *testing.T, repo EventJoinRepository, member *model.EventMember) JoinOutcome {
	t.Helper()
	if member.Status == "" {
		member.Status = model.EventMemberStatusConfirmed
	}
	outcome, err := repo.Join(context.Background(), member, model.JoinNotification{})
	if err != nil {
		t.Fatalf("Join() returned error: %v", err)
	}
	return outcome
}

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

			joinConfirmedTestMember(t, repo, member)

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
		joinConfirmedTestMember(t, repo, member)

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
		joinConfirmedTestMember(t, repo, member)

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
			joinConfirmedTestMember(t, repo, member)
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
	joinConfirmedTestMember(t, repo, loggedInMember)

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
	joinConfirmedTestMember(t, repo, anonymousMember)

	// 未確定（pending）の申込は参加者一覧に含めない（ADR-0046）。
	insertTestPendingEventMember(t, db, eventID, uuid.NewString()+"@example.com", 4)

	members, err := repo.ListMembers(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListMembers() returned error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2（未確定の申込が混入している）", len(members))
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

// TestEventJoinPostgres_ListRecipients_ExcludesPending は通知宛先に
// 未確定（pending）の申込を含めないことを検証する（ADR-0046）。
func TestEventJoinPostgres_ListRecipients_ExcludesPending(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)

	confirmedMail := uuid.NewString() + "@example.com"
	pendingMail := uuid.NewString() + "@example.com"
	insertTestEventMember(t, db, eventID, confirmedMail, 1)
	insertTestPendingEventMember(t, db, eventID, pendingMail, 1)

	recipients, err := repo.ListRecipients(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListRecipients() returned error: %v", err)
	}
	if len(recipients) != 1 || recipients[0].MailAddress != confirmedMail {
		t.Errorf("recipients = %#v, want [%q]", recipients, confirmedMail)
	}
}

// TestEventJoinPostgres_Join_ExcludesPendingFromCapacity は定員判定に
// 未確定（pending）の申込を含めないことを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_ExcludesPendingFromCapacity(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)
	const updateCapacity = `UPDATE events SET capacity = $2 WHERE id = $1`
	if _, err := db.ExecContext(context.Background(), updateCapacity, eventID, 1); err != nil {
		t.Fatalf("update capacity: %v", err)
	}

	// pending の1名は定員を埋めない。
	insertTestPendingEventMember(t, db, eventID, uuid.NewString()+"@example.com", 1)

	newMember := func() *model.EventMember {
		return &model.EventMember{
			EventID:     eventID,
			Username:    "参加者",
			MailAddress: uuid.NewString() + "@example.com",
			PartySize:   1,
			Categories: []model.MemberCategory{
				{Category: "大人", HeadCount: 1},
			},
		}
	}

	first := newMember()
	joinConfirmedTestMember(t, repo, first)

	second := newMember()
	second.Status = model.EventMemberStatusConfirmed
	outcome, err := repo.Join(context.Background(), second, model.JoinNotification{})
	if err != nil {
		t.Fatalf("2件目の Join() returned error: %v", err)
	}
	if outcome != JoinOutcomeCapacityFull {
		t.Fatalf("2件目の Join() outcome = %q, want %q", outcome, JoinOutcomeCapacityFull)
	}
}

// TestEventJoinPostgres_Join_PendingAndConfirm は未確定で登録した申込が
// 一覧・人数に含まれず、確認トークンで確定すると含まれるようになることを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_PendingAndConfirm(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)
	eventRepo := NewEventRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)

	mailAddress := uuid.NewString() + "@example.com"
	tokenHash := uuid.NewString()
	member := &model.EventMember{
		EventID:     eventID,
		Username:    "ゲスト花子",
		MailAddress: mailAddress,
		PartySize:   2,
		Categories: []model.MemberCategory{
			{Category: "大人", HeadCount: 2},
		},
		Status:                     model.EventMemberStatusPending,
		ConfirmationTokenHash:      tokenHash,
		ConfirmationTokenExpiresAt: time.Now().Add(time.Hour),
	}
	notification := model.JoinNotification{
		ConfirmationSubject: "確認メールの件名",
		ConfirmationBody:    "確認メールの本文",
	}

	outcome, err := repo.Join(context.Background(), member, notification)
	if err != nil {
		t.Fatalf("Join() returned error: %v", err)
	}
	if outcome != JoinOutcomePending {
		t.Fatalf("Join() outcome = %q, want %q", outcome, JoinOutcomePending)
	}

	var (
		status    string
		tokenCol  sql.NullString
		expiresAt sql.NullTime
	)
	const selectMember = `
	SELECT status, confirmation_token_hash, confirmation_token_expires_at
	FROM event_members
	WHERE id = $1
	`
	if err := db.QueryRowContext(context.Background(), selectMember, member.ID).
		Scan(&status, &tokenCol, &expiresAt); err != nil {
		t.Fatalf("select pending member: %v", err)
	}
	if status != model.EventMemberStatusPending {
		t.Errorf("status = %q, want %q", status, model.EventMemberStatusPending)
	}
	if !tokenCol.Valid || tokenCol.String != tokenHash {
		t.Errorf("confirmation_token_hash = %#v, want %q", tokenCol, tokenHash)
	}
	if !expiresAt.Valid {
		t.Error("confirmation_token_expires_at が NULL")
	}

	// 未確定は参加者一覧・参加人数に含めない。
	members, err := repo.ListMembers(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListMembers() returned error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("ListMembers() = %d件, want 0（pending は含めない）", len(members))
	}
	gotEvent, err := eventRepo.GetByID(context.Background(), eventID.String())
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}
	if gotEvent.ParticipantCount != 0 {
		t.Errorf("ParticipantCount = %d, want 0（pending は含めない）", gotEvent.ParticipantCount)
	}

	// 確認メールが outbox に予約されている。
	var (
		recipientKind  string
		recipientEmail string
		subject        string
	)
	const selectOutbox = `
	SELECT recipient_kind, recipient_email, subject
	FROM event_notification_outbox
	WHERE event_id = $1
	ORDER BY created_at
	LIMIT 1
	`
	if err := db.QueryRowContext(context.Background(), selectOutbox, eventID).
		Scan(&recipientKind, &recipientEmail, &subject); err != nil {
		t.Fatalf("select join outbox: %v", err)
	}
	if recipientKind != model.NotificationOutboxRecipientKindEmail {
		t.Errorf("recipient_kind = %q, want %q", recipientKind, model.NotificationOutboxRecipientKindEmail)
	}
	if recipientEmail != mailAddress {
		t.Errorf("recipient_email = %q, want %q", recipientEmail, mailAddress)
	}
	if subject != notification.ConfirmationSubject {
		t.Errorf("subject = %q, want %q", subject, notification.ConfirmationSubject)
	}

	// 不正なトークンでは確定できない。
	if _, err := repo.ConfirmJoin(context.Background(), eventID, "wrong-token"); !errors.Is(err, ErrConfirmationTokenInvalid) {
		t.Fatalf("ConfirmJoin(wrong) error = %v, want ErrConfirmationTokenInvalid", err)
	}

	confirmedAt, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash)
	if err != nil {
		t.Fatalf("ConfirmJoin() returned error: %v", err)
	}
	if confirmedAt.IsZero() {
		t.Error("confirmedAt がゼロ値")
	}

	const selectConfirmed = `
	SELECT status, confirmation_token_hash IS NULL, confirmation_token_expires_at IS NULL
	FROM event_members
	WHERE id = $1
	`
	var (
		tokenIsNull   bool
		expiresIsNull bool
	)
	if err := db.QueryRowContext(context.Background(), selectConfirmed, member.ID).
		Scan(&status, &tokenIsNull, &expiresIsNull); err != nil {
		t.Fatalf("select confirmed member: %v", err)
	}
	if status != model.EventMemberStatusConfirmed {
		t.Errorf("status = %q, want %q", status, model.EventMemberStatusConfirmed)
	}
	if !tokenIsNull || !expiresIsNull {
		t.Error("確定後もトークン列が残っている")
	}

	// 確定後は参加者一覧・参加人数に含まれる。
	members, err = repo.ListMembers(context.Background(), eventID)
	if err != nil {
		t.Fatalf("ListMembers() returned error: %v", err)
	}
	if len(members) != 1 || members[0].Username != "ゲスト花子" {
		t.Errorf("ListMembers() = %#v, want ゲスト花子1件", members)
	}
	gotEvent, err = eventRepo.GetByID(context.Background(), eventID.String())
	if err != nil {
		t.Fatalf("GetByID() returned error: %v", err)
	}
	if gotEvent.ParticipantCount != 2 {
		t.Errorf("ParticipantCount = %d, want 2", gotEvent.ParticipantCount)
	}

	// 使用済みトークンでは再度確定できない。
	if _, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash); !errors.Is(err, ErrConfirmationTokenInvalid) {
		t.Fatalf("2回目の ConfirmJoin() error = %v, want ErrConfirmationTokenInvalid", err)
	}
}

// TestEventJoinPostgres_Join_PendingRequiresNotification は、pending の申込に
// 確認メールの件名・本文が無い場合に登録を拒否することを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_PendingRequiresNotification(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)

	member := &model.EventMember{
		EventID:                    eventID,
		Username:                   "ゲスト",
		MailAddress:                uuid.NewString() + "@example.com",
		PartySize:                  1,
		Categories:                 []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:                     model.EventMemberStatusPending,
		ConfirmationTokenHash:      uuid.NewString(),
		ConfirmationTokenExpiresAt: time.Now().Add(time.Hour),
	}
	if _, err := repo.Join(context.Background(), member, model.JoinNotification{}); err == nil {
		t.Fatal("Join() error = nil, want error（確認メール文面が無い pending）")
	}

	const countMembers = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
	var count int
	if err := db.QueryRowContext(context.Background(), countMembers, eventID).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 0 {
		t.Errorf("member count = %d, want 0", count)
	}
}

// TestEventJoinPostgres_Join_DuplicateEmail は同一メールアドレスの再申込が
// 新規行を作らず、案内メールを outbox に予約することを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_DuplicateEmail(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)

	mailAddress := uuid.NewString() + "@example.com"
	confirmed := &model.EventMember{
		EventID:     eventID,
		Username:    "先着",
		MailAddress: mailAddress,
		PartySize:   1,
		Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:      model.EventMemberStatusConfirmed,
	}
	joinConfirmedTestMember(t, repo, confirmed)

	duplicate := &model.EventMember{
		EventID:                    eventID,
		Username:                   "後着",
		MailAddress:                mailAddress,
		PartySize:                  1,
		Categories:                 []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:                     model.EventMemberStatusPending,
		ConfirmationTokenHash:      uuid.NewString(),
		ConfirmationTokenExpiresAt: time.Now().Add(time.Hour),
	}
	notification := model.JoinNotification{
		ConfirmationSubject: "確認",
		ConfirmationBody:    "確認本文",
		NoticeSubject:       "すでに申し込まれています",
		NoticeBody:          "案内本文",
	}
	outcome, err := repo.Join(context.Background(), duplicate, notification)
	if err != nil {
		t.Fatalf("Join() returned error: %v", err)
	}
	if outcome != JoinOutcomeAlreadyJoined {
		t.Fatalf("Join() outcome = %q, want %q", outcome, JoinOutcomeAlreadyJoined)
	}

	const countMembers = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
	var count int
	if err := db.QueryRowContext(context.Background(), countMembers, eventID).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 1 {
		t.Errorf("member count = %d, want 1（重複行を作らない）", count)
	}

	var subject string
	const selectOutbox = `
	SELECT subject
	FROM event_notification_outbox
	WHERE event_id = $1
	AND recipient_kind = 'email'
	AND lower(recipient_email) = lower($2)
	`
	if err := db.QueryRowContext(context.Background(), selectOutbox, eventID, mailAddress).Scan(&subject); err != nil {
		t.Fatalf("select notice outbox: %v", err)
	}
	if subject != notification.NoticeSubject {
		t.Errorf("subject = %q, want %q", subject, notification.NoticeSubject)
	}
}

// TestEventJoinPostgres_Join_DuplicateEmail_LoggedIn は、認証済みメールのログイン参加が
// 同一メールの確定済み申込と重複した場合、案内メールを予約せず
// JoinOutcomeAlreadyJoinedSelf を返すことを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_DuplicateEmail_LoggedIn(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)

	mailAddress := uuid.NewString() + "@example.com"
	// ゲストとして確定済みの行（profile_id NULL）を用意する。
	insertTestEventMember(t, db, eventID, mailAddress, 1)

	profileID := insertTestProfile(t, db)
	member := &model.EventMember{
		EventID:     eventID,
		ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
		Username:    "ログイン参加者",
		MailAddress: mailAddress,
		PartySize:   1,
		Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:      model.EventMemberStatusConfirmed,
	}
	outcome, err := repo.Join(context.Background(), member, model.JoinNotification{
		NoticeSubject: "すでに申し込まれています",
		NoticeBody:    "案内本文",
	})
	if err != nil {
		t.Fatalf("Join() returned error: %v", err)
	}
	if outcome != JoinOutcomeAlreadyJoinedSelf {
		t.Fatalf("Join() outcome = %q, want %q", outcome, JoinOutcomeAlreadyJoinedSelf)
	}

	const countMembers = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
	var count int
	if err := db.QueryRowContext(context.Background(), countMembers, eventID).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 1 {
		t.Errorf("member count = %d, want 1（重複行を作らない）", count)
	}

	const countEmailOutbox = `
	SELECT COUNT(*)
	FROM event_notification_outbox
	WHERE event_id = $1
	AND recipient_kind = 'email'
	`
	if err := db.QueryRowContext(context.Background(), countEmailOutbox, eventID).Scan(&count); err != nil {
		t.Fatalf("count email outbox: %v", err)
	}
	if count != 0 {
		t.Errorf("email outbox count = %d, want 0（ログイン中の重複は案内メールを送らない）", count)
	}
}

// TestEventJoinPostgres_Join_LoggedInTakeover はログイン参加が同一メールの
// pending 行を置き換えて即時確定することを検証する（ADR-0046）。
func TestEventJoinPostgres_Join_LoggedInTakeover(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	eventID := insertTestEvent(t, db, ownerID)
	insertTestCost(t, db, eventID, "大人", 500)

	mailAddress := uuid.NewString() + "@example.com"
	pending := &model.EventMember{
		EventID:                    eventID,
		Username:                   "ゲスト",
		MailAddress:                mailAddress,
		PartySize:                  1,
		Categories:                 []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:                     model.EventMemberStatusPending,
		ConfirmationTokenHash:      uuid.NewString(),
		ConfirmationTokenExpiresAt: time.Now().Add(time.Hour),
	}
	if outcome, err := repo.Join(context.Background(), pending, model.JoinNotification{
		ConfirmationSubject: "確認",
		ConfirmationBody:    "確認本文",
	}); err != nil || outcome != JoinOutcomePending {
		t.Fatalf("pending Join() = (%q, %v), want (%q, nil)", outcome, err, JoinOutcomePending)
	}

	profileID := insertTestProfile(t, db)
	loggedIn := &model.EventMember{
		EventID:     eventID,
		ProfileID:   uuid.NullUUID{UUID: profileID, Valid: true},
		Username:    "ログイン参加者",
		MailAddress: mailAddress,
		PartySize:   1,
		Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
		Status:      model.EventMemberStatusConfirmed,
	}
	outcome, err := repo.Join(context.Background(), loggedIn, model.JoinNotification{})
	if err != nil {
		t.Fatalf("logged-in Join() returned error: %v", err)
	}
	if outcome != JoinOutcomeConfirmed {
		t.Fatalf("logged-in Join() outcome = %q, want %q", outcome, JoinOutcomeConfirmed)
	}

	const query = `
	SELECT status, profile_id
	FROM event_members
	WHERE event_id = $1
	AND lower(mail_address) = lower($2)
	`
	var (
		status     string
		gotProfile uuid.NullUUID
	)
	if err := db.QueryRowContext(context.Background(), query, eventID, mailAddress).
		Scan(&status, &gotProfile); err != nil {
		t.Fatalf("select member: %v", err)
	}
	if status != model.EventMemberStatusConfirmed {
		t.Errorf("status = %q, want %q", status, model.EventMemberStatusConfirmed)
	}
	if !gotProfile.Valid || gotProfile.UUID != profileID {
		t.Errorf("profile_id = %#v, want %v", gotProfile, profileID)
	}

	var count int
	const countMembers = `SELECT COUNT(*) FROM event_members WHERE event_id = $1`
	if err := db.QueryRowContext(context.Background(), countMembers, eventID).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 1 {
		t.Errorf("member count = %d, want 1（pending 行を置き換える）", count)
	}

	// 置き換えで無効になる確認メールの予約は残さない。
	const countEmailOutbox = `
	SELECT COUNT(*)
	FROM event_notification_outbox
	WHERE event_id = $1
	AND recipient_kind = 'email'
	AND lower(recipient_email) = lower($2)
	AND status = 'pending'
	`
	if err := db.QueryRowContext(context.Background(), countEmailOutbox, eventID, mailAddress).Scan(&count); err != nil {
		t.Fatalf("count email outbox: %v", err)
	}
	if count != 0 {
		t.Errorf("pending email outbox count = %d, want 0", count)
	}
}

// TestEventJoinPostgres_ConfirmJoin_Errors は確定時の期限切れ・定員・イベント状態の
// エラーを検証する（ADR-0046）。
func TestEventJoinPostgres_ConfirmJoin_Errors(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)
	ownerID := insertTestProfile(t, db)

	newPending := func(t *testing.T, eventID uuid.UUID, tokenHash string, expiresAt time.Time) *model.EventMember {
		t.Helper()
		member := &model.EventMember{
			EventID:                    eventID,
			Username:                   "ゲスト",
			MailAddress:                uuid.NewString() + "@example.com",
			PartySize:                  1,
			Categories:                 []model.MemberCategory{{Category: "大人", HeadCount: 1}},
			Status:                     model.EventMemberStatusPending,
			ConfirmationTokenHash:      tokenHash,
			ConfirmationTokenExpiresAt: expiresAt,
		}
		if outcome, err := repo.Join(context.Background(), member, model.JoinNotification{
			ConfirmationSubject: "確認",
			ConfirmationBody:    "確認本文",
		}); err != nil || outcome != JoinOutcomePending {
			t.Fatalf("pending Join() = (%q, %v), want (%q, nil)", outcome, err, JoinOutcomePending)
		}
		return member
	}

	t.Run("期限切れのトークンは ErrConfirmationTokenInvalid", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		tokenHash := uuid.NewString()
		newPending(t, eventID, tokenHash, time.Now().Add(-time.Minute))

		if _, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash); !errors.Is(err, ErrConfirmationTokenInvalid) {
			t.Fatalf("ConfirmJoin() error = %v, want ErrConfirmationTokenInvalid", err)
		}
	})

	t.Run("定員超過は ErrEventCapacityFull", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		const updateCapacity = `UPDATE events SET capacity = $2 WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), updateCapacity, eventID, 1); err != nil {
			t.Fatalf("update capacity: %v", err)
		}
		tokenHash := uuid.NewString()
		newPending(t, eventID, tokenHash, time.Now().Add(time.Hour))

		// pending は定員に含めないため、confirmed の1名はまだ登録できる。
		joinConfirmedTestMember(t, repo, &model.EventMember{
			EventID:     eventID,
			Username:    "先着",
			MailAddress: uuid.NewString() + "@example.com",
			PartySize:   1,
			Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
			Status:      model.EventMemberStatusConfirmed,
		})

		if _, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash); !errors.Is(err, ErrEventCapacityFull) {
			t.Fatalf("ConfirmJoin() error = %v, want ErrEventCapacityFull", err)
		}
	})

	t.Run("取消済みイベントは ErrEventCancelled", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		tokenHash := uuid.NewString()
		newPending(t, eventID, tokenHash, time.Now().Add(time.Hour))

		const cancel = `UPDATE events SET cancelled_at = now() WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), cancel, eventID); err != nil {
			t.Fatalf("cancel event: %v", err)
		}
		if _, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash); !errors.Is(err, ErrEventCancelled) {
			t.Fatalf("ConfirmJoin() error = %v, want ErrEventCancelled", err)
		}
	})

	t.Run("終了済みイベントは ErrEventEnded", func(t *testing.T) {
		eventID := insertTestEvent(t, db, ownerID)
		insertTestCost(t, db, eventID, "大人", 500)
		tokenHash := uuid.NewString()
		newPending(t, eventID, tokenHash, time.Now().Add(time.Hour))

		const end = `UPDATE events SET event_date = now() - interval '2 hours', end_date = now() - interval '1 hour' WHERE id = $1`
		if _, err := db.ExecContext(context.Background(), end, eventID); err != nil {
			t.Fatalf("end event: %v", err)
		}
		if _, err := repo.ConfirmJoin(context.Background(), eventID, tokenHash); !errors.Is(err, ErrEventEnded) {
			t.Fatalf("ConfirmJoin() error = %v, want ErrEventEnded", err)
		}
	})
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
		joinConfirmedTestMember(t, repo, member)

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
		member.Status = model.EventMemberStatusConfirmed
		_, err := repo.Join(context.Background(), member, model.JoinNotification{})
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
		member.Status = model.EventMemberStatusConfirmed
		if _, err := repo.Join(context.Background(), member, model.JoinNotification{}); !errors.Is(err, ErrCategoryNotFound) {
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
		member.Status = model.EventMemberStatusConfirmed
		if _, err := repo.Join(context.Background(), member, model.JoinNotification{}); !errors.Is(err, ErrDuplicateCategory) {
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
	INSERT INTO event_members(id, event_id, profile_id, username, mail_address, party_size, status)
	VALUES($1, $2, $3, $4, $5, $6, 'confirmed')
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

// TestEventJoinPostgres_Join_ApplicationDeadline は申込期限・終了日時の判定を検証する（ADR-0029）。
// 期限経過後・終了日時（end_date）経過後の申込は拒否し、期限前・期限なし（終了前）は受け付ける。
func TestEventJoinPostgres_Join_ApplicationDeadline(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventJoinRepository(db)

	ownerID := insertTestProfile(t, db)
	now := time.Now()

	// setupEvent は費用カテゴリ「大人」を持つイベントを、指定の申込期限で作成する。
	// endDate がゼロ値以外なら setTestEventDates で日時を上書きする。
	// CHECK 制約 end_date >= event_date を満たすため event_date も同時に過去に設定する。
	setupEvent := func(t *testing.T, deadline sql.NullTime, endDate time.Time) uuid.UUID {
		t.Helper()
		eventID := insertTestEventWithApplicationDeadline(t, db, ownerID, deadline)
		if !endDate.IsZero() {
			setTestEventDates(t, db, eventID, now.Add(-2*time.Hour), endDate)
		}
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
		// endDate はゼロ値以外なら setupEvent 後に setTestEventDates で日時を上書きする。
		endDate time.Time
		wantErr error
	}{
		{
			name:     "正常: 申込期限前は申し込める",
			deadline: sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		},
		{
			name:     "正常: 期限なしで終了前は申し込める",
			deadline: sql.NullTime{},
			endDate:  now.Add(time.Hour),
		},
		{
			name:     "異常: 期限なしで終了日時経過後は ErrEventEnded を返す",
			deadline: sql.NullTime{},
			endDate:  now.Add(-time.Hour),
			wantErr:  ErrEventEnded,
		},
		{
			name:     "異常: 申込期限経過後は ErrDeadlinePassed を返す",
			deadline: sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
			wantErr:  ErrDeadlinePassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventID := setupEvent(t, tt.deadline, tt.endDate)
			member := &model.EventMember{
				EventID:     eventID,
				Username:    "参加者",
				MailAddress: uuid.NewString() + "@example.com",
				PartySize:   1,
				Categories:  []model.MemberCategory{{Category: "大人", HeadCount: 1}},
				Status:      model.EventMemberStatusConfirmed,
			}

			_, err := repo.Join(context.Background(), member, model.JoinNotification{})

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
