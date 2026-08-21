package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// TestEscapeLike は escapeLike が ILIKE のワイルドカード(% _)と
// エスケープ文字(\)を正しく無効化し、純粋な部分一致文字列に変換することを検証する。
func TestEscapeLike(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "特殊文字なしはそのまま",
			input: "サクラ観察会",
			want:  "サクラ観察会",
		},
		{
			name:  "空文字はそのまま",
			input: "",
			want:  "",
		},
		{
			name:  "パーセントをエスケープ",
			input: "50%",
			want:  `50\%`,
		},
		{
			name:  "アンダースコアをエスケープ",
			input: "a_b",
			want:  `a\_b`,
		},
		{
			name:  "バックスラッシュを二重化",
			input: `back\slash`,
			want:  `back\\slash`,
		},
		{
			name:  "複数の特殊文字を同時にエスケープ",
			input: `1_0%_x`,
			want:  `1\_0\%\_x`,
		},
		{
			// バックスラッシュを先に処理しないと、% のエスケープで挿入した \ が
			// 二重化されてしまう。順序が正しいことを確認する。
			name:  "バックスラッシュとパーセントの複合",
			input: `\%`,
			want:  `\\\%`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeLike(tt.input); got != tt.want {
				t.Errorf("escapeLike(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeSearchText は NormalizeSearchText(NFKC) が半角/全角の表記ゆれを
// 吸収すること、および ひらがな↔カタカナは変換しないことを検証する。
func TestNormalizeSearchText(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "全角数字→半角数字", input: "２０２６", want: "2026"},
		{name: "全角英字→半角英字", input: "ＡＢＣ", want: "ABC"},
		{name: "半角カナ→全角カナ", input: "ｶﾀｶﾅ", want: "カタカナ"},
		{name: "全角パーセント→半角パーセント", input: "５０％", want: "50%"},
		{name: "半角英数字はそのまま", input: "abc123", want: "abc123"},
		{name: "ひらがなはカタカナ化しない", input: "さくら", want: "さくら"},
		{name: "空文字はそのまま", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSearchText(tt.input); got != tt.want {
				t.Errorf("NormalizeSearchText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBuildSearchWhere は buildSearchWhere が各キーワードを5フィールド OR の
// 1グループとし、グループ間を AND で連結すること、タグ条件を EXISTS + IN の1グループに
// まとめること、プレースホルダを startIdx から連番で割り当てること（キーワード→タグ→地域の順）、
// ILIKE パターン・タグID・地域引数を順序どおり生成することを検証する。
// 開催状況(status)は statusClauses 由来の条件式を OR で連結した1グループになり、
// プレースホルダを消費しないこと（ADR-0027）も併せて検証する。
// 地域(location)は e.location への ILIKE 条件を OR で連結した1グループになり、
// プレースホルダを消費すること（ADR-0028）も併せて検証する。
func TestBuildSearchWhere(t *testing.T) {
	t.Helper()

	tests := []struct {
		name     string
		filter   model.EventSearchFilter
		startIdx int
		// wantContains は生成 WHERE 句に必ず含まれるべき部分文字列。
		wantContains []string
		// wantNotContains は含まれてはならない部分文字列（AND 連結の確認等）。
		wantNotContains []string
		wantAndCount    int // ") AND (" の出現回数（キーワードグループ数-1。タグ条件との連結は含まない）
		wantArgs        []any
	}{
		{
			name:     "単一キーワード: $1 が5フィールド(normalize適用)へ展開され AND を含まない",
			filter:   model.EventSearchFilter{Keywords: []string{"桜"}},
			startIdx: 1,
			wantContains: []string{
				"normalize(e.title, NFKC) ILIKE $1",
				"normalize(e.description, NFKC) ILIKE $1",
				"normalize(p.display_name, NFKC) ILIKE $1",
				"normalize(e.location, NFKC) ILIKE $1",
				"normalize(it.event_item, NFKC) ILIKE $1",
			},
			wantAndCount: 0,
			wantArgs:     []any{"%桜%"},
		},
		{
			name:            "複数キーワード: 連番プレースホルダと AND 連結",
			filter:          model.EventSearchFilter{Keywords: []string{"桜", "東京"}},
			startIdx:        1,
			wantContains:    []string{"ILIKE $1", "ILIKE $2", ") AND ("},
			wantNotContains: []string{"ILIKE $3"},
			wantAndCount:    1,
			wantArgs:        []any{"%桜%", "%東京%"},
		},
		{
			name:         "startIdx オフセット: limit/offset を後続に置くため $3 から開始",
			filter:       model.EventSearchFilter{Keywords: []string{"a", "b"}},
			startIdx:     3,
			wantContains: []string{"ILIKE $3", "ILIKE $4"},
			wantAndCount: 1,
			wantArgs:     []any{"%a%", "%b%"},
		},
		{
			name:         "特殊文字を含むキーワードはエスケープされてパターン化される",
			filter:       model.EventSearchFilter{Keywords: []string{"50%"}},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{`%50\%%`},
		},
		{
			name:         "全角数字は NFKC 正規化で半角化されてパターン化される",
			filter:       model.EventSearchFilter{Keywords: []string{"２０２６"}},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{"%2026%"},
		},
		{
			name:         "全角パーセントは NFKC で ASCII 化された後 LIKE エスケープされる",
			filter:       model.EventSearchFilter{Keywords: []string{"５０％"}},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{`%50\%%`},
		},
		{
			name:         "タグのみ1件: EXISTS + IN で1プレースホルダになり ILIKE を含まない",
			filter:       model.EventSearchFilter{TagIDs: []string{"tag-1"}},
			startIdx:     1,
			wantContains: []string{"EXISTS (SELECT 1 FROM event_tags et WHERE et.event_id = e.id AND et.tag_id IN ($1))"},
			wantNotContains: []string{
				"ILIKE",
			},
			wantAndCount: 0,
			wantArgs:     []any{"tag-1"},
		},
		{
			name:         "タグ複数: IN にカンマ区切りで並び OR セマンティクスになる",
			filter:       model.EventSearchFilter{TagIDs: []string{"tag-1", "tag-2"}},
			startIdx:     1,
			wantContains: []string{"et.tag_id IN ($1, $2)"},
			wantAndCount: 0,
			wantArgs:     []any{"tag-1", "tag-2"},
		},
		{
			name:     "キーワード1件+タグ2件: プレースホルダはキーワード→タグの順で連番になる",
			filter:   model.EventSearchFilter{Keywords: []string{"桜"}, TagIDs: []string{"tag-1", "tag-2"}},
			startIdx: 1,
			wantContains: []string{
				"normalize(e.title, NFKC) ILIKE $1",
				") AND EXISTS (",
				"et.tag_id IN ($2, $3)",
			},
			wantAndCount: 0, // キーワードグループは1つのみのため ") AND (" は無い（キーワード-タグ間は ") AND EXISTS (" で判定）
			wantArgs:     []any{"%桜%", "tag-1", "tag-2"},
		},
		{
			name:         "startIdx=3 でキーワード1+タグ1: ILIKE $3, IN ($4) になる",
			filter:       model.EventSearchFilter{Keywords: []string{"a"}, TagIDs: []string{"tag-1"}},
			startIdx:     3,
			wantContains: []string{"ILIKE $3", "IN ($4)"},
			wantAndCount: 0,
			wantArgs:     []any{"%a%", "tag-1"},
		},
		{
			name:     "status単独(upcoming): プレースホルダを消費せずOR句にならない",
			filter:   model.EventSearchFilter{Statuses: []model.EventStatus{model.EventStatusUpcoming}},
			startIdx: 1,
			wantContains: []string{
				"(e.event_date > now())",
			},
			wantNotContains: []string{"ILIKE", "EXISTS", "OR"},
			wantAndCount:    0,
			wantArgs:        []any{},
		},
		{
			name: "status複数(upcoming,ongoing): OR で連結され ongoing は括弧で囲まれる",
			filter: model.EventSearchFilter{
				Statuses: []model.EventStatus{model.EventStatusUpcoming, model.EventStatusOngoing},
			},
			startIdx: 1,
			wantContains: []string{
				"(e.event_date > now() OR (e.event_date <= now() AND e.end_date >= now()))",
			},
			wantAndCount: 0,
			wantArgs:     []any{},
		},
		{
			name: "status3値すべて: 定義順で3条件がORで連結される",
			filter: model.EventSearchFilter{
				Statuses: []model.EventStatus{
					model.EventStatusUpcoming, model.EventStatusOngoing, model.EventStatusEnded,
				},
			},
			startIdx: 1,
			wantContains: []string{
				"(e.event_date > now() OR (e.event_date <= now() AND e.end_date >= now()) OR e.end_date < now())",
			},
			wantAndCount: 0,
			wantArgs:     []any{},
		},
		{
			name: "キーワード1+タグ1+status1: プレースホルダはキーワード→タグの順のみでstatusは消費せず末尾にAND連結される",
			filter: model.EventSearchFilter{
				Keywords: []string{"a"},
				TagIDs:   []string{"tag-1"},
				Statuses: []model.EventStatus{model.EventStatusUpcoming},
			},
			startIdx: 1,
			wantContains: []string{
				"ILIKE $1",
				"IN ($2)",
				") AND EXISTS (",
				") AND (e.event_date > now())",
			},
			wantNotContains: []string{"ILIKE $3", "IN ($3)"},
			wantAndCount:    1,
			wantArgs:        []any{"%a%", "tag-1"},
		},
		{
			name:     "location単独1件: プレースホルダを消費し normalize(e.location, NFKC) ILIKE $1 になる（ORなし）",
			filter:   model.EventSearchFilter{Locations: []string{"東京都"}},
			startIdx: 1,
			wantContains: []string{
				"normalize(e.location, NFKC) ILIKE $1",
			},
			wantNotContains: []string{"EXISTS", " OR "},
			wantAndCount:    0,
			wantArgs:        []any{"%東京都%"},
		},
		{
			name:     "location複数: 連番プレースホルダがORで連結される（他条件とはANDにならない）",
			filter:   model.EventSearchFilter{Locations: []string{"東京都", "神奈川県"}},
			startIdx: 1,
			wantContains: []string{
				"(normalize(e.location, NFKC) ILIKE $1 OR normalize(e.location, NFKC) ILIKE $2)",
			},
			wantAndCount: 0,
			wantArgs:     []any{"%東京都%", "%神奈川県%"},
		},
		{
			name: "キーワード1+タグ1+status1+location1: プレースホルダはキーワード→タグ→locationの順で連番になりstatusは消費しない",
			filter: model.EventSearchFilter{
				Keywords:  []string{"a"},
				TagIDs:    []string{"tag-1"},
				Statuses:  []model.EventStatus{model.EventStatusUpcoming},
				Locations: []string{"東京都"},
			},
			startIdx: 1,
			wantContains: []string{
				"ILIKE $1",
				"IN ($2)",
				") AND EXISTS (",
				") AND (e.event_date > now())",
				") AND (normalize(e.location, NFKC) ILIKE $3)",
			},
			wantNotContains: []string{"ILIKE $4", "IN ($3)"},
			wantAndCount:    2,
			wantArgs:        []any{"%a%", "tag-1", "%東京都%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args, err := buildSearchWhere(tt.filter, tt.startIdx)
			if err != nil {
				t.Fatalf("buildSearchWhere() returned error: %v", err)
			}

			for _, sub := range tt.wantContains {
				if !strings.Contains(where, sub) {
					t.Errorf("WHERE 句に %q が含まれるべき\nwhere=%s", sub, where)
				}
			}
			for _, sub := range tt.wantNotContains {
				if strings.Contains(where, sub) {
					t.Errorf("WHERE 句に %q が含まれるべきではない\nwhere=%s", sub, where)
				}
			}
			// グループ間の連結は ") AND (" で判定する（EXISTS 内部の AND と区別するため）。
			// キーワード群とタグ条件の連結は ") AND EXISTS (" になりこのカウントには含まれない。
			if got := strings.Count(where, ") AND ("); got != tt.wantAndCount {
				t.Errorf("グループ AND 連結回数: got %d, want %d\nwhere=%s", got, tt.wantAndCount, where)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args: got %#v, want %#v", args, tt.wantArgs)
			}
		})
	}
}

// TestBuildSearchWhere_UnknownStatus は statusClauses に無い開催状況が渡された場合に
// エラーを返し、WHERE 句を組み立てないことを確認する（ADR-0027）。
// service 層の normalizeStatuses を経由すれば到達しない経路の防御にあたる。
func TestBuildSearchWhere_UnknownStatus(t *testing.T) {
	tests := []struct {
		name   string
		status model.EventStatus
	}{
		{name: "statusClauses に無い値", status: model.EventStatus("cancelled")},
		{name: "空文字", status: model.EventStatus("")},
		{name: "大文字表記は別の値として扱われる", status: model.EventStatus("UPCOMING")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := model.EventSearchFilter{Statuses: []model.EventStatus{tt.status}}

			where, args, err := buildSearchWhere(filter, 1)
			if err == nil {
				t.Fatalf("未知の status ではエラーを返すべき\nwhere=%s", where)
			}
			// エラー時に組み立て途中の WHERE 句を返すと、呼び出し元が不正な SQL を発行しうる。
			if where != "" {
				t.Errorf("エラー時の WHERE 句は空であるべき: got %q", where)
			}
			if args != nil {
				t.Errorf("エラー時の引数は nil であるべき: got %#v", args)
			}
			if !strings.Contains(err.Error(), string(tt.status)) {
				t.Errorf("エラーメッセージに未知の値が含まれるべき: %v", err)
			}
		})
	}
}

// insertTestTag はテスト用の tags 行を1件作成する。
// name は tags.name/normalized_name の UNIQUE 制約（かつ VARCHAR(30)）を避けるため、
// prefix に短い一意サフィックスを付けた名前を採番して使う。ソート順の検証で prefix
// 同士の大小関係が変わらないよう、サフィックスは末尾に付与する。返り値は実際に保存した名前。
func insertTestTag(t *testing.T, db *sql.DB, prefix string) (uuid.UUID, string) {
	t.Helper()

	id := uuid.New()
	name := fmt.Sprintf("%s-%s", prefix, uuid.NewString()[:8])
	const insertTag = `
	INSERT INTO tags(id, name, normalized_name)
	VALUES($1, $2, $3)
	`
	if _, err := db.ExecContext(context.Background(), insertTag, id, name, name); err != nil {
		t.Fatalf("insert test tag: %v", err)
	}
	return id, name
}

// linkEventTag はテスト用に event_tags 行を1件作成する。
func linkEventTag(t *testing.T, db *sql.DB, eventID, tagID uuid.UUID) {
	t.Helper()

	const insertEventTag = `
	INSERT INTO event_tags(event_id, tag_id)
	VALUES($1, $2)
	`
	if _, err := db.ExecContext(context.Background(), insertEventTag, eventID, tagID); err != nil {
		t.Fatalf("insert test event_tag: %v", err)
	}
}

// TestEventPostgres_GetByID_Tags は GetByID が紐づくタグを name 昇順で返すこと、
// タグが0件のイベントでは Tags が nil ではなく空スライスになることを検証する。
func TestEventPostgres_GetByID_Tags(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	t.Run("複数タグを紐づけたイベントは name 昇順で返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		tagBID, tagBName := insertTestTag(t, db, "外来生物")
		tagAID, tagAName := insertTestTag(t, db, "きのこ")
		linkEventTag(t, db, eventID, tagBID)
		linkEventTag(t, db, eventID, tagAID)

		got, err := repo.GetByID(context.Background(), eventID.String())
		if err != nil {
			t.Fatalf("GetByID() returned error: %v", err)
		}

		want := []model.TagResponse{
			{ID: tagAID.String(), Name: tagAName},
			{ID: tagBID.String(), Name: tagBName},
		}
		if !reflect.DeepEqual(got.Tags, want) {
			t.Errorf("Tags = %#v, want %#v", got.Tags, want)
		}
	})

	t.Run("タグ0件のイベントは空スライスを返す", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		got, err := repo.GetByID(context.Background(), eventID.String())
		if err != nil {
			t.Fatalf("GetByID() returned error: %v", err)
		}

		if got.Tags == nil {
			t.Error("Tags = nil, want empty slice")
		}
		if len(got.Tags) != 0 {
			t.Errorf("Tags = %#v, want empty", got.Tags)
		}
	})
}

// insertTestEventMember はテスト用の event_members 行を1件作成する。
// profile_id は匿名参加を想定して常に NULL にする。同一イベントに複数件作成する場合は
// (event_id, lower(mail_address)) の UNIQUE 制約を避けるため mailAddress を呼び出し側で
// ユニークにする。
func insertTestEventMember(t *testing.T, db *sql.DB, eventID uuid.UUID, mailAddress string, partySize int) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertMember = `
	INSERT INTO event_members(id, event_id, profile_id, username, mail_address, party_size)
	VALUES($1, $2, NULL, $3, $4, $5)
	`
	if _, err := db.ExecContext(context.Background(), insertMember, id, eventID, "テスト参加者", mailAddress, partySize); err != nil {
		t.Fatalf("insert test event member: %v", err)
	}
	return id
}

// TestEventPostgres_GetByID_ParticipantCount は GetByID の ParticipantCount が
// event_members.party_size の合計になること、申込0件のイベントでは0になること、
// 他イベントの申込が合算されないことを検証する（ADR-0024）。
func TestEventPostgres_GetByID_ParticipantCount(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	t.Run("申込0件のイベントはParticipantCountが0になる", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		got, err := repo.GetByID(context.Background(), eventID.String())
		if err != nil {
			t.Fatalf("GetByID() returned error: %v", err)
		}

		if got.ParticipantCount != 0 {
			t.Errorf("ParticipantCount = %d, want 0", got.ParticipantCount)
		}
	})

	t.Run("複数申込のparty_sizeを合計する", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)

		insertTestEventMember(t, db, eventID, fmt.Sprintf("%s@example.com", uuid.NewString()), 2)
		insertTestEventMember(t, db, eventID, fmt.Sprintf("%s@example.com", uuid.NewString()), 3)

		got, err := repo.GetByID(context.Background(), eventID.String())
		if err != nil {
			t.Fatalf("GetByID() returned error: %v", err)
		}

		if got.ParticipantCount != 5 {
			t.Errorf("ParticipantCount = %d, want 5", got.ParticipantCount)
		}
	})

	t.Run("別イベントの申込は合算されない", func(t *testing.T) {
		eventID := insertTestEvent(t, db, profileID)
		otherEventID := insertTestEvent(t, db, profileID)

		insertTestEventMember(t, db, eventID, fmt.Sprintf("%s@example.com", uuid.NewString()), 2)
		insertTestEventMember(t, db, otherEventID, fmt.Sprintf("%s@example.com", uuid.NewString()), 10)

		got, err := repo.GetByID(context.Background(), eventID.String())
		if err != nil {
			t.Fatalf("GetByID() returned error: %v", err)
		}

		if got.ParticipantCount != 2 {
			t.Errorf("ParticipantCount = %d, want 2（別イベントの申込が混入している）", got.ParticipantCount)
		}
	})
}

// findSummaryByID は summaries から id に一致する要素を返す。
// 見つかった場合は ok が true になる。
func findSummaryByID(summaries []model.EventSummary, id string) (summary model.EventSummary, ok bool) {
	for _, s := range summaries {
		if s.ID == id {
			return s, true
		}
	}
	return model.EventSummary{}, false
}

// TestEventPostgres_ListSummaries_Tags は ListSummaries が attachTagsToSummaries 経由で
// 紐づくタグを name 昇順で返すこと、タグの無いイベントでは Tags が nil
// (JSON では omitempty で省略) になることを検証する。
func TestEventPostgres_ListSummaries_Tags(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	taggedEventID := insertTestEvent(t, db, profileID)
	tagBID, tagBName := insertTestTag(t, db, "外来生物")
	tagAID, tagAName := insertTestTag(t, db, "きのこ")
	linkEventTag(t, db, taggedEventID, tagBID)
	linkEventTag(t, db, taggedEventID, tagAID)

	untaggedEventID := insertTestEvent(t, db, profileID)

	// 既存データを含む全件を確実に取得できるよう、件数を数えてから limit に使う。
	total, err := repo.CountSummaries(context.Background())
	if err != nil {
		t.Fatalf("CountSummaries() returned error: %v", err)
	}

	got, err := repo.ListSummaries(context.Background(), "created_at", "desc", total+10, 0)
	if err != nil {
		t.Fatalf("ListSummaries() returned error: %v", err)
	}

	taggedSummary, ok := findSummaryByID(got, taggedEventID.String())
	if !ok {
		t.Fatalf("ListSummaries() result does not contain event %s", taggedEventID)
	}
	wantTags := []model.TagResponse{
		{ID: tagAID.String(), Name: tagAName},
		{ID: tagBID.String(), Name: tagBName},
	}
	if !reflect.DeepEqual(taggedSummary.Tags, wantTags) {
		t.Errorf("Tags = %#v, want %#v", taggedSummary.Tags, wantTags)
	}

	untaggedSummary, ok := findSummaryByID(got, untaggedEventID.String())
	if !ok {
		t.Fatalf("ListSummaries() result does not contain event %s", untaggedEventID)
	}
	if untaggedSummary.Tags != nil {
		t.Errorf("Tags = %#v, want nil", untaggedSummary.Tags)
	}
}

// TestEventPostgres_SearchSummaries_Tags は SearchSummaries が attachTagsToSummaries 経由で
// 紐づくタグを name 昇順で返すこと、タグの無いイベントでは Tags が nil
// (JSON では omitempty で省略) になることを検証する。
func TestEventPostgres_SearchSummaries_Tags(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	taggedEventID := insertTestEvent(t, db, profileID)
	tagBID, tagBName := insertTestTag(t, db, "外来生物")
	tagAID, tagAName := insertTestTag(t, db, "きのこ")
	linkEventTag(t, db, taggedEventID, tagBID)
	linkEventTag(t, db, taggedEventID, tagAID)

	untaggedEventID := insertTestEvent(t, db, profileID)

	// insertTestEvent は title を固定値で作成するため、その語をキーワードに検索する。
	// 既存データを含む一致件数を数えてから limit に使う。
	filter := model.EventSearchFilter{Keywords: []string{"テストイベント"}}
	total, err := repo.CountSearchSummaries(context.Background(), filter)
	if err != nil {
		t.Fatalf("CountSearchSummaries() returned error: %v", err)
	}

	got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", total+10, 0)
	if err != nil {
		t.Fatalf("SearchSummaries() returned error: %v", err)
	}

	taggedSummary, ok := findSummaryByID(got, taggedEventID.String())
	if !ok {
		t.Fatalf("SearchSummaries() result does not contain event %s", taggedEventID)
	}
	wantTags := []model.TagResponse{
		{ID: tagAID.String(), Name: tagAName},
		{ID: tagBID.String(), Name: tagBName},
	}
	if !reflect.DeepEqual(taggedSummary.Tags, wantTags) {
		t.Errorf("Tags = %#v, want %#v", taggedSummary.Tags, wantTags)
	}

	untaggedSummary, ok := findSummaryByID(got, untaggedEventID.String())
	if !ok {
		t.Fatalf("SearchSummaries() result does not contain event %s", untaggedEventID)
	}
	if untaggedSummary.Tags != nil {
		t.Errorf("Tags = %#v, want nil", untaggedSummary.Tags)
	}
}

// insertTestEventWithTitle はテスト用の events 行を1件、指定したタイトルで作成する。
// insertTestEvent は title を固定値("テストイベント")で作成するため、キーワード検索で
// 特定のイベントだけを一致させたいテスト（AND 検索の検証等）ではこちらを使う。
func insertTestEventWithTitle(t *testing.T, db *sql.DB, profileID uuid.UUID, title string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	eventDate := time.Now()
	const insertEvent = `
	INSERT INTO events(id, profile_id, title, event_date, end_date)
	VALUES($1, $2, $3, $4, $5)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertEvent,
		id,
		profileID,
		title,
		eventDate,
		eventDate,
	); err != nil {
		t.Fatalf("insert test event: %v", err)
	}
	return id
}

// insertTestEventWithEndDate はテスト用の events 行を1件、指定した end_date で作成する。
// insertTestEvent は event_date/end_date とも現在時刻固定のため、終了済み/開催中を
// 作り分けたいテスト（マイページのapplied/attended境界の検証等）ではこちらを使う。
// event_date は events_end_date_after_event_date 制約(end_date >= event_date)を満たすよう
// endDate の1時間前に固定する（endDate が過去でも未来でも成立する）。
func insertTestEventWithEndDate(t *testing.T, db *sql.DB, profileID uuid.UUID, endDate time.Time) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertEvent = `
	INSERT INTO events(id, profile_id, title, event_date, end_date)
	VALUES($1, $2, $3, $4, $5)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertEvent,
		id,
		profileID,
		"テストイベント",
		endDate.Add(-1*time.Hour),
		endDate,
	); err != nil {
		t.Fatalf("insert test event: %v", err)
	}
	return id
}

// insertTestEventWithDates はテスト用の events 行を1件、指定した event_date/end_date で作成する。
// insertTestEventWithEndDate は event_date を endDate の1時間前に固定するため、
// event_date と end_date の両方を独立に指定したいテスト（開催中(ongoing)の境界検証等）では
// こちらを使う。呼び出し元は events_end_date_after_event_date 制約(end_date >= event_date)を
// 満たす値を渡すこと。
func insertTestEventWithDates(t *testing.T, db *sql.DB, profileID uuid.UUID, eventDate, endDate time.Time) uuid.UUID {
	t.Helper()

	id := uuid.New()
	const insertEvent = `
	INSERT INTO events(id, profile_id, title, event_date, end_date)
	VALUES($1, $2, $3, $4, $5)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertEvent,
		id,
		profileID,
		"テストイベント",
		eventDate,
		endDate,
	); err != nil {
		t.Fatalf("insert test event: %v", err)
	}
	return id
}

// insertTestEventWithLocation はテスト用の events 行を1件、指定した location で作成する。
func insertTestEventWithLocation(t *testing.T, db *sql.DB, profileID uuid.UUID, location string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	eventDate := time.Now()
	const insertEvent = `
	INSERT INTO events(id, profile_id, title, location, event_date, end_date)
	VALUES($1, $2, $3, $4, $5, $6)
	`
	if _, err := db.ExecContext(
		context.Background(),
		insertEvent,
		id,
		profileID,
		"テストイベント",
		location,
		eventDate,
		eventDate,
	); err != nil {
		t.Fatalf("insert test event: %v", err)
	}
	return id
}

// insertTestMember はテスト用の event_members 行を1件作成する（参加申込を表す）。
// profileID.Valid が false の場合は匿名申込（profile_id は NULL）として登録する。
func insertTestMember(t *testing.T, db *sql.DB, eventID uuid.UUID, profileID uuid.NullUUID) uuid.UUID {
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
		"参加者",
		uuid.NewString()+"@example.com",
		1,
	); err != nil {
		t.Fatalf("insert test member: %v", err)
	}
	return id
}

// deleteTestMember はテスト用の event_members 行を削除する（参加キャンセル/leave 相当）。
func deleteTestMember(t *testing.T, db *sql.DB, memberID uuid.UUID) {
	t.Helper()

	const deleteMember = `DELETE FROM event_members WHERE id = $1`
	if _, err := db.ExecContext(context.Background(), deleteMember, memberID); err != nil {
		t.Fatalf("delete test member: %v", err)
	}
}

// countOccurrences は summaries 内で id に一致する要素の個数を返す。
// EXISTS によるタグ絞り込みで、複数タグに一致する行が重複して現れないことの検証に使う。
func countOccurrences(summaries []model.EventSummary, id string) int {
	n := 0
	for _, s := range summaries {
		if s.ID == id {
			n++
		}
	}
	return n
}

// TestEventPostgres_SearchSummaries_TagFilter は SearchSummaries/CountSearchSummaries の
// タグ絞り込み(OR)・キーワードとの併用(AND)・EXISTS による重複排除を検証する。
//
// insertTestTag は毎回新規の UUID を採番するため、このテストで使うタグIDを条件に含む検索は
// このテスト内で作成したイベントにしか一致しない。そのため他テストの既存データと干渉しない。
func TestEventPostgres_SearchSummaries_TagFilter(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	t.Run("OR検索: いずれかのタグを持つイベントが該当し、両方持つイベントも重複しない", func(t *testing.T) {
		tagAID, _ := insertTestTag(t, db, "タグA")
		tagBID, _ := insertTestTag(t, db, "タグB")

		eventAOnly := insertTestEvent(t, db, profileID)
		linkEventTag(t, db, eventAOnly, tagAID)

		eventBOnly := insertTestEvent(t, db, profileID)
		linkEventTag(t, db, eventBOnly, tagBID)

		eventBoth := insertTestEvent(t, db, profileID)
		linkEventTag(t, db, eventBoth, tagAID)
		linkEventTag(t, db, eventBoth, tagBID)

		eventNoTag := insertTestEvent(t, db, profileID)

		filter := model.EventSearchFilter{TagIDs: []string{tagAID.String(), tagBID.String()}}

		count, err := repo.CountSearchSummaries(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountSearchSummaries() returned error: %v", err)
		}

		// 自前で作成したデータが全部入るよう count+10 を limit にする。
		got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", count+10, 0)
		if err != nil {
			t.Fatalf("SearchSummaries() returned error: %v", err)
		}
		// CountSearchSummaries と SearchSummaries の件数が整合すること（重複カウントされないこと）。
		if len(got) != count {
			t.Errorf("SearchSummaries() 件数 = %d, CountSearchSummaries() = %d: 不整合", len(got), count)
		}

		if _, ok := findSummaryByID(got, eventAOnly.String()); !ok {
			t.Errorf("タグAのみのイベントが結果に含まれるべき")
		}
		if _, ok := findSummaryByID(got, eventBOnly.String()); !ok {
			t.Errorf("タグBのみのイベントが結果に含まれるべき")
		}
		if _, ok := findSummaryByID(got, eventNoTag.String()); ok {
			t.Errorf("タグ無しイベントは結果に含まれるべきではない")
		}
		if n := countOccurrences(got, eventBoth.String()); n != 1 {
			t.Errorf("両方のタグを持つイベントは1回だけ現れるべき: got %d 回", n)
		}
	})

	t.Run("AND検索: キーワードとタグを併用するとタグは一致してもキーワードが不一致なら除外される", func(t *testing.T) {
		tagCID, _ := insertTestTag(t, db, "タグC")
		keyword := "AND検索固有" + uuid.NewString()[:8]

		eventMatch := insertTestEventWithTitle(t, db, profileID, keyword+"についてのイベント")
		linkEventTag(t, db, eventMatch, tagCID)

		// title は insertTestEvent 固定値("テストイベント")のためキーワードを含まない。
		eventTagOnly := insertTestEvent(t, db, profileID)
		linkEventTag(t, db, eventTagOnly, tagCID)

		filter := model.EventSearchFilter{Keywords: []string{keyword}, TagIDs: []string{tagCID.String()}}

		got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries() returned error: %v", err)
		}

		if _, ok := findSummaryByID(got, eventMatch.String()); !ok {
			t.Errorf("キーワード・タグ両方に一致するイベントが結果に含まれるべき")
		}
		if _, ok := findSummaryByID(got, eventTagOnly.String()); ok {
			t.Errorf("タグは一致してもキーワードが不一致のイベントは結果に含まれるべきではない")
		}
	})

	t.Run("存在しないタグIDで検索すると0件になる", func(t *testing.T) {
		filter := model.EventSearchFilter{TagIDs: []string{uuid.New().String()}}

		count, err := repo.CountSearchSummaries(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountSearchSummaries() returned error: %v", err)
		}
		if count != 0 {
			t.Errorf("CountSearchSummaries() = %d, want 0", count)
		}

		got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries() returned error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("SearchSummaries() 件数 = %d, want 0", len(got))
		}
	})
}

// TestEventPostgres_SearchSummaries_StatusFilter は SearchSummaries/CountSearchSummaries の
// 開催状況(status)絞り込み（3値それぞれ・複数指定のOR・q/tagIdとのAND・3値すべての指定が
// status未指定と一致すること）を検証する（ADR-0027）。
//
// insertTestTag は毎回新規の UUID を採番するため、このテストで使うスコープ用タグを条件に含む
// 検索はこのテスト内で作成したイベントにしか一致しない。そのため他テストの既存データと干渉しない。
// event_date/end_date は now() 基準の相対値で作成し、境界から十分離すことで時間経過による
// テストのflakinessを避ける。
func TestEventPostgres_SearchSummaries_StatusFilter(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)
	scopeTagID, _ := insertTestTag(t, db, "ステータス絞り込みスコープ")

	now := time.Now()
	upcomingEvent := insertTestEventWithDates(t, db, profileID, now.Add(24*time.Hour), now.Add(48*time.Hour))
	linkEventTag(t, db, upcomingEvent, scopeTagID)

	ongoingEvent := insertTestEventWithDates(t, db, profileID, now.Add(-24*time.Hour), now.Add(24*time.Hour))
	linkEventTag(t, db, ongoingEvent, scopeTagID)

	endedEvent := insertTestEventWithDates(t, db, profileID, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	linkEventTag(t, db, endedEvent, scopeTagID)

	// scopeFilter はスコープ用タグで絞り込んだ上で status を上書きするベース。
	scopeFilter := func(statuses ...model.EventStatus) model.EventSearchFilter {
		return model.EventSearchFilter{
			TagIDs:   []string{scopeTagID.String()},
			Statuses: statuses,
		}
	}

	runAndAssert := func(t *testing.T, filter model.EventSearchFilter, wantIDs, dontWantIDs []uuid.UUID) {
		t.Helper()

		got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries() returned error: %v", err)
		}
		for _, id := range wantIDs {
			if _, ok := findSummaryByID(got, id.String()); !ok {
				t.Errorf("%s が結果に含まれるべき", id)
			}
		}
		for _, id := range dontWantIDs {
			if _, ok := findSummaryByID(got, id.String()); ok {
				t.Errorf("%s は結果に含まれるべきではない", id)
			}
		}

		count, err := repo.CountSearchSummaries(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountSearchSummaries() returned error: %v", err)
		}
		if count != len(got) {
			t.Errorf("CountSearchSummaries() = %d, SearchSummaries() 件数 = %d: 不整合", count, len(got))
		}
	}

	t.Run("upcoming: event_date > now() のイベントのみ該当する", func(t *testing.T) {
		runAndAssert(t,
			scopeFilter(model.EventStatusUpcoming),
			[]uuid.UUID{upcomingEvent},
			[]uuid.UUID{ongoingEvent, endedEvent},
		)
	})

	t.Run("ongoing: event_date <= now() かつ end_date >= now() のイベントのみ該当する", func(t *testing.T) {
		runAndAssert(t,
			scopeFilter(model.EventStatusOngoing),
			[]uuid.UUID{ongoingEvent},
			[]uuid.UUID{upcomingEvent, endedEvent},
		)
	})

	t.Run("ended: end_date < now() のイベントのみ該当する", func(t *testing.T) {
		runAndAssert(t,
			scopeFilter(model.EventStatusEnded),
			[]uuid.UUID{endedEvent},
			[]uuid.UUID{upcomingEvent, ongoingEvent},
		)
	})

	t.Run("複数指定はOR: upcomingとendedを指定するとongoingだけ除外される", func(t *testing.T) {
		runAndAssert(t,
			scopeFilter(model.EventStatusUpcoming, model.EventStatusEnded),
			[]uuid.UUID{upcomingEvent, endedEvent},
			[]uuid.UUID{ongoingEvent},
		)
	})

	t.Run("3値すべて指定はstatus未指定と結果が一致する（排他かつ網羅）", func(t *testing.T) {
		allStatuses := scopeFilter(model.EventStatusUpcoming, model.EventStatusOngoing, model.EventStatusEnded)
		noStatus := scopeFilter()

		gotAll, err := repo.SearchSummaries(context.Background(), allStatuses, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries(3値すべて) returned error: %v", err)
		}
		gotNone, err := repo.SearchSummaries(context.Background(), noStatus, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries(status未指定) returned error: %v", err)
		}
		if len(gotAll) != len(gotNone) {
			t.Fatalf("件数が不一致: 3値すべて=%d, status未指定=%d", len(gotAll), len(gotNone))
		}
		for _, s := range gotAll {
			if _, ok := findSummaryByID(gotNone, s.ID); !ok {
				t.Errorf("3値すべての結果にあるイベント %s が status未指定の結果に含まれるべき", s.ID)
			}
		}

		countAll, err := repo.CountSearchSummaries(context.Background(), allStatuses)
		if err != nil {
			t.Fatalf("CountSearchSummaries(3値すべて) returned error: %v", err)
		}
		countNone, err := repo.CountSearchSummaries(context.Background(), noStatus)
		if err != nil {
			t.Fatalf("CountSearchSummaries(status未指定) returned error: %v", err)
		}
		if countAll != countNone {
			t.Errorf("CountSearchSummaries(3値すべて) = %d, CountSearchSummaries(status未指定) = %d: 不一致", countAll, countNone)
		}
	})

	t.Run("q・tagIdとのAND: statusに一致してもキーワードが不一致なら除外される", func(t *testing.T) {
		keyword := "status_and_test" + uuid.NewString()[:8]

		matchEvent := insertTestEventWithTitle(t, db, profileID, keyword+"の終了済みイベント")
		linkEventTag(t, db, matchEvent, scopeTagID)
		if _, err := db.ExecContext(context.Background(),
			`UPDATE events SET event_date = $2, end_date = $3 WHERE id = $1`,
			matchEvent, now.Add(-48*time.Hour), now.Add(-24*time.Hour),
		); err != nil {
			t.Fatalf("update test event dates: %v", err)
		}

		tagOnlyEvent := insertTestEventWithTitle(t, db, profileID, "キーワード不一致イベント")
		linkEventTag(t, db, tagOnlyEvent, scopeTagID)
		if _, err := db.ExecContext(context.Background(),
			`UPDATE events SET event_date = $2, end_date = $3 WHERE id = $1`,
			tagOnlyEvent, now.Add(-48*time.Hour), now.Add(-24*time.Hour),
		); err != nil {
			t.Fatalf("update test event dates: %v", err)
		}

		filter := model.EventSearchFilter{
			Keywords: []string{keyword},
			TagIDs:   []string{scopeTagID.String()},
			Statuses: []model.EventStatus{model.EventStatusEnded},
		}
		runAndAssert(t, filter,
			[]uuid.UUID{matchEvent},
			[]uuid.UUID{tagOnlyEvent, upcomingEvent, ongoingEvent},
		)
	})
}

// TestEventPostgres_SearchSummaries_LocationFilter は SearchSummaries/CountSearchSummaries の
// 地域(location)絞り込み（部分一致・NFKC正規化・複数指定のOR・q/tagIdとのAND）を検証する（ADR-0028）。
//
// scope は毎回一意な文字列を location の先頭に付与し、このテストで作成したイベントにしか
// 一致しないようにする。そのため他テストの既存データと干渉しない。
func TestEventPostgres_SearchSummaries_LocationFilter(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)
	scope := "地域絞り込みスコープ" + uuid.NewString()[:8]

	tokyoEvent := insertTestEventWithLocation(t, db, profileID, scope+"東京都新宿区")
	kanagawaEvent := insertTestEventWithLocation(t, db, profileID, scope+"神奈川県横浜市")
	osakaEvent := insertTestEventWithLocation(t, db, profileID, scope+"大阪府大阪市")
	noLocationEvent := insertTestEvent(t, db, profileID)

	runAndAssert := func(t *testing.T, filter model.EventSearchFilter, wantIDs, dontWantIDs []uuid.UUID) {
		t.Helper()

		got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("SearchSummaries() returned error: %v", err)
		}
		for _, id := range wantIDs {
			if _, ok := findSummaryByID(got, id.String()); !ok {
				t.Errorf("%s が結果に含まれるべき", id)
			}
		}
		for _, id := range dontWantIDs {
			if _, ok := findSummaryByID(got, id.String()); ok {
				t.Errorf("%s は結果に含まれるべきではない", id)
			}
		}

		count, err := repo.CountSearchSummaries(context.Background(), filter)
		if err != nil {
			t.Fatalf("CountSearchSummaries() returned error: %v", err)
		}
		if count != len(got) {
			t.Errorf("CountSearchSummaries() = %d, SearchSummaries() 件数 = %d: 不整合", count, len(got))
		}
	}

	t.Run("部分一致: locationの一部を指定すると該当イベントのみ返る", func(t *testing.T) {
		runAndAssert(t,
			model.EventSearchFilter{Locations: []string{scope + "東京都"}},
			[]uuid.UUID{tokyoEvent},
			[]uuid.UUID{kanagawaEvent, osakaEvent, noLocationEvent},
		)
	})

	t.Run("複数指定はOR: 東京都と神奈川県を指定すると大阪府は除外される", func(t *testing.T) {
		runAndAssert(t,
			model.EventSearchFilter{Locations: []string{scope + "東京都", scope + "神奈川県"}},
			[]uuid.UUID{tokyoEvent, kanagawaEvent},
			[]uuid.UUID{osakaEvent, noLocationEvent},
		)
	})

	t.Run("NFKC正規化: 全角/半角の表記ゆれを吸収する", func(t *testing.T) {
		fullwidthEvent := insertTestEventWithLocation(t, db, profileID, scope+"ＴＯＫＹＯ地区")

		runAndAssert(t,
			model.EventSearchFilter{Locations: []string{scope + "TOKYO"}},
			[]uuid.UUID{fullwidthEvent},
			[]uuid.UUID{tokyoEvent, kanagawaEvent, osakaEvent},
		)
	})

	t.Run("q・tagIdとのAND: locationに一致してもキーワードが不一致なら除外される", func(t *testing.T) {
		keyword := "location_and_test" + uuid.NewString()[:8]
		tagID, _ := insertTestTag(t, db, "地域AND検証タグ")
		andScopeLocation := scope + "AND検証東京都"

		matchEvent := insertTestEventWithTitle(t, db, profileID, keyword+"の説明")
		linkEventTag(t, db, matchEvent, tagID)
		if _, err := db.ExecContext(context.Background(),
			`UPDATE events SET location = $2 WHERE id = $1`, matchEvent, andScopeLocation,
		); err != nil {
			t.Fatalf("update test event location: %v", err)
		}

		locationOnlyEvent := insertTestEventWithLocation(t, db, profileID, andScopeLocation)
		linkEventTag(t, db, locationOnlyEvent, tagID)

		filter := model.EventSearchFilter{
			Keywords:  []string{keyword},
			TagIDs:    []string{tagID.String()},
			Locations: []string{andScopeLocation},
		}
		runAndAssert(t, filter,
			[]uuid.UUID{matchEvent},
			[]uuid.UUID{locationOnlyEvent, tokyoEvent, kanagawaEvent, osakaEvent},
		)
	})
}

// TestEventPostgres_SearchSummaries_CombinedFilters は q・tagId・status・location を
// 同時指定した場合に buildSearchWhere が生成する SQL を実際に PostgreSQL へ発行し、AND で
// 結合されることを検証する。プレースホルダはKeywords→TagIDs→Locationsの順で連番になり
// Statuses は消費しない（ADR-0027, ADR-0028）。
//
// スコープ用タグ・キーワード・location はいずれも毎回一意な値を使うため、このテストで
// 作成したイベントにしか一致しない。日時は now() 基準の相対値で作成し、時間経過による
// テストのflakinessを避ける。
func TestEventPostgres_SearchSummaries_CombinedFilters(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)
	scopeTagID, _ := insertTestTag(t, db, "4条件同時指定スコープ")
	keyword := "combined_filter_test" + uuid.NewString()[:8]
	scopeLocation := "4条件同時指定スコープ" + uuid.NewString()[:8] + "東京都"

	now := time.Now()
	ongoingEventDate, ongoingEndDate := now.Add(-24*time.Hour), now.Add(24*time.Hour)
	endedEventDate, endedEndDate := now.Add(-48*time.Hour), now.Add(-24*time.Hour)

	setDates := func(t *testing.T, eventID uuid.UUID, eventDate, endDate time.Time) {
		t.Helper()
		if _, err := db.ExecContext(context.Background(),
			`UPDATE events SET event_date = $2, end_date = $3 WHERE id = $1`,
			eventID, eventDate, endDate,
		); err != nil {
			t.Fatalf("update test event dates: %v", err)
		}
	}
	setLocation := func(t *testing.T, eventID uuid.UUID, location string) {
		t.Helper()
		if _, err := db.ExecContext(context.Background(),
			`UPDATE events SET location = $2 WHERE id = $1`, eventID, location,
		); err != nil {
			t.Fatalf("update test event location: %v", err)
		}
	}

	// matchEvent は4条件(q・tagId・status=ongoing・location)すべてに一致する。
	matchEvent := insertTestEventWithTitle(t, db, profileID, keyword+"の説明")
	linkEventTag(t, db, matchEvent, scopeTagID)
	setDates(t, matchEvent, ongoingEventDate, ongoingEndDate)
	setLocation(t, matchEvent, scopeLocation)

	// missingKeywordEvent はタイトルにkeywordを含まず、qで除外される。
	missingKeywordEvent := insertTestEventWithTitle(t, db, profileID, "キーワード不一致イベント")
	linkEventTag(t, db, missingKeywordEvent, scopeTagID)
	setDates(t, missingKeywordEvent, ongoingEventDate, ongoingEndDate)
	setLocation(t, missingKeywordEvent, scopeLocation)

	// missingTagEvent はscopeTagIDに紐づかず、tagIdで除外される。
	missingTagEvent := insertTestEventWithTitle(t, db, profileID, keyword+"のタグ不一致イベント")
	setDates(t, missingTagEvent, ongoingEventDate, ongoingEndDate)
	setLocation(t, missingTagEvent, scopeLocation)

	// missingStatusEvent は終了済みで、status=ongoingで除外される。
	missingStatusEvent := insertTestEventWithTitle(t, db, profileID, keyword+"のステータス不一致イベント")
	linkEventTag(t, db, missingStatusEvent, scopeTagID)
	setDates(t, missingStatusEvent, endedEventDate, endedEndDate)
	setLocation(t, missingStatusEvent, scopeLocation)

	// missingLocationEvent はlocationが一致せず、locationで除外される。
	missingLocationEvent := insertTestEventWithTitle(t, db, profileID, keyword+"の地域不一致イベント")
	linkEventTag(t, db, missingLocationEvent, scopeTagID)
	setDates(t, missingLocationEvent, ongoingEventDate, ongoingEndDate)
	setLocation(t, missingLocationEvent, "地域不一致"+uuid.NewString()[:8])

	filter := model.EventSearchFilter{
		Keywords:  []string{keyword},
		TagIDs:    []string{scopeTagID.String()},
		Statuses:  []model.EventStatus{model.EventStatusOngoing},
		Locations: []string{scopeLocation},
	}

	got, err := repo.SearchSummaries(context.Background(), filter, "created_at", "desc", 100, 0)
	if err != nil {
		t.Fatalf("SearchSummaries() returned error: %v", err)
	}
	if _, ok := findSummaryByID(got, matchEvent.String()); !ok {
		t.Errorf("%s が結果に含まれるべき", matchEvent)
	}
	for _, id := range []uuid.UUID{missingKeywordEvent, missingTagEvent, missingStatusEvent, missingLocationEvent} {
		if _, ok := findSummaryByID(got, id.String()); ok {
			t.Errorf("%s は結果に含まれるべきではない", id)
		}
	}

	count, err := repo.CountSearchSummaries(context.Background(), filter)
	if err != nil {
		t.Fatalf("CountSearchSummaries() returned error: %v", err)
	}
	if count != len(got) {
		t.Errorf("CountSearchSummaries() = %d, SearchSummaries() 件数 = %d: 不整合", count, len(got))
	}
}

// TestEventPostgres_ListMySummaries_Hosted は hosted 種別が自分が主催したイベントのみを
// 返すこと（他人のイベントは含まない）、キャンセル済み・終了済みのイベントも含むことを検証する。
//
// 件数検証は共有 DB 上の既存データと干渉しないよう、このテストで作成したプロフィールに
// 紐づくイベント ID が結果に含まれる/含まれないかで判定する。
func TestEventPostgres_ListMySummaries_Hosted(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)
	otherProfileID := insertTestProfile(t, db)

	upcomingEvent := insertTestEvent(t, db, profileID)

	cancelledEvent := insertTestEvent(t, db, profileID)
	if _, err := repo.CancelWithNotification(context.Background(), cancelledEvent, "件名", "本文"); err != nil {
		t.Fatalf("CancelWithNotification() returned error: %v", err)
	}

	endedEvent := insertTestEventWithEndDate(t, db, profileID, time.Now().Add(-24*time.Hour))

	otherEvent := insertTestEvent(t, db, otherProfileID)

	got, err := repo.ListMySummaries(context.Background(), model.MyEventFilter{
		ProfileID: profileID,
		Kind:      model.MyEventKindHosted,
	}, "created_at", "desc", 100, 0)
	if err != nil {
		t.Fatalf("ListMySummaries() returned error: %v", err)
	}

	for _, wantID := range []uuid.UUID{upcomingEvent, cancelledEvent, endedEvent} {
		if _, ok := findSummaryByID(got, wantID.String()); !ok {
			t.Errorf("主催イベント %s が結果に含まれるべき", wantID)
		}
	}
	if _, ok := findSummaryByID(got, otherEvent.String()); ok {
		t.Errorf("他人が主催したイベントは結果に含まれるべきではない")
	}

	cancelledSummary, ok := findSummaryByID(got, cancelledEvent.String())
	if !ok {
		t.Fatalf("cancelledEvent が結果に含まれていない")
	}
	if cancelledSummary.CancelledAt == nil {
		t.Error("キャンセル済みイベントの CancelledAt が nil, want non-nil")
	}
}

// TestEventPostgres_ListMySummaries_AppliedAttended は applied/attended 種別の境界
// （end_date と now() の比較）、匿名申込（profile_id NULL）が誰の一覧にも出ないこと、
// キャンセル済みイベントも end_date で振り分けられること（ADR-0024）、
// 参加行削除（leave 相当）後は applied にも attended にも出ないことを検証する。
func TestEventPostgres_ListMySummaries_AppliedAttended(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	ownerID := insertTestProfile(t, db)
	meID := insertTestProfile(t, db)
	otherID := insertTestProfile(t, db)

	futureEvent := insertTestEventWithEndDate(t, db, ownerID, time.Now().Add(24*time.Hour))
	pastEvent := insertTestEventWithEndDate(t, db, ownerID, time.Now().Add(-24*time.Hour))
	anonOnlyEvent := insertTestEventWithEndDate(t, db, ownerID, time.Now().Add(24*time.Hour))

	memberFuture := insertTestMember(t, db, futureEvent, uuid.NullUUID{UUID: meID, Valid: true})
	insertTestMember(t, db, pastEvent, uuid.NullUUID{UUID: meID, Valid: true})
	insertTestMember(t, db, futureEvent, uuid.NullUUID{UUID: otherID, Valid: true})
	insertTestMember(t, db, anonOnlyEvent, uuid.NullUUID{}) // 匿名申込(profile_id NULL)

	listApplied := func(t *testing.T, profileID uuid.UUID) []model.EventSummary {
		t.Helper()
		got, err := repo.ListMySummaries(context.Background(), model.MyEventFilter{
			ProfileID: profileID,
			Kind:      model.MyEventKindApplied,
		}, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("ListMySummaries(applied) returned error: %v", err)
		}
		return got
	}
	listAttended := func(t *testing.T, profileID uuid.UUID) []model.EventSummary {
		t.Helper()
		got, err := repo.ListMySummaries(context.Background(), model.MyEventFilter{
			ProfileID: profileID,
			Kind:      model.MyEventKindAttended,
		}, "created_at", "desc", 100, 0)
		if err != nil {
			t.Fatalf("ListMySummaries(attended) returned error: %v", err)
		}
		return got
	}

	t.Run("applied: end_dateが未到来かつ自分の申込があるイベントのみ含む", func(t *testing.T) {
		got := listApplied(t, meID)
		if _, ok := findSummaryByID(got, futureEvent.String()); !ok {
			t.Error("申込済み・未終了のイベントが含まれるべき")
		}
		if _, ok := findSummaryByID(got, pastEvent.String()); ok {
			t.Error("終了済みのイベントは applied に含まれるべきではない")
		}
		if _, ok := findSummaryByID(got, anonOnlyEvent.String()); ok {
			t.Error("自分が申込していないイベントは含まれるべきではない")
		}
	})

	t.Run("attended: end_dateを過ぎており自分の申込があるイベントのみ含む", func(t *testing.T) {
		got := listAttended(t, meID)
		if _, ok := findSummaryByID(got, pastEvent.String()); !ok {
			t.Error("申込済み・終了済みのイベントが含まれるべき")
		}
		if _, ok := findSummaryByID(got, futureEvent.String()); ok {
			t.Error("未終了のイベントは attended に含まれるべきではない")
		}
	})

	t.Run("匿名申込は他人(owner)の一覧にも出ない", func(t *testing.T) {
		got := listApplied(t, ownerID)
		if _, ok := findSummaryByID(got, anonOnlyEvent.String()); ok {
			t.Error("匿名申込のみのイベントは他人の一覧にも含まれるべきではない")
		}
	})

	t.Run("キャンセル済みイベントも end_date で applied/attended に振り分けられる", func(t *testing.T) {
		cancelledFutureEvent := insertTestEventWithEndDate(t, db, ownerID, time.Now().Add(24*time.Hour))
		cancelledPastEvent := insertTestEventWithEndDate(t, db, ownerID, time.Now().Add(-24*time.Hour))
		insertTestMember(t, db, cancelledFutureEvent, uuid.NullUUID{UUID: meID, Valid: true})
		insertTestMember(t, db, cancelledPastEvent, uuid.NullUUID{UUID: meID, Valid: true})

		if _, err := repo.CancelWithNotification(context.Background(), cancelledFutureEvent, "件名", "本文"); err != nil {
			t.Fatalf("CancelWithNotification() returned error: %v", err)
		}
		if _, err := repo.CancelWithNotification(context.Background(), cancelledPastEvent, "件名", "本文"); err != nil {
			t.Fatalf("CancelWithNotification() returned error: %v", err)
		}

		appliedSummary, ok := findSummaryByID(listApplied(t, meID), cancelledFutureEvent.String())
		if !ok {
			t.Error("end_dateが未到来のキャンセル済みイベントは applied に含まれるべき")
		} else if appliedSummary.CancelledAt == nil {
			t.Error("CancelledAt が nil, want non-nil")
		}

		attendedSummary, ok := findSummaryByID(listAttended(t, meID), cancelledPastEvent.String())
		if !ok {
			t.Error("end_dateを過ぎたキャンセル済みイベントは attended に含まれるべき")
		} else if attendedSummary.CancelledAt == nil {
			t.Error("CancelledAt が nil, want non-nil")
		}
	})

	t.Run("参加行を削除(leave相当)すると applied にも attended にも出ない", func(t *testing.T) {
		deleteTestMember(t, db, memberFuture)

		if _, ok := findSummaryByID(listApplied(t, meID), futureEvent.String()); ok {
			t.Error("参加取消後は applied に含まれるべきではない")
		}
		if _, ok := findSummaryByID(listAttended(t, meID), futureEvent.String()); ok {
			t.Error("参加取消後は attended にも含まれるべきではない")
		}
	})
}

// TestEventPostgres_CountMyEventKinds は hosted/applied/attended の件数を1クエリで
// 正しく返すことを検証する。新規プロフィールで隔離しているため、既存データの件数に左右されない。
func TestEventPostgres_CountMyEventKinds(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)
	otherProfileID := insertTestProfile(t, db)

	insertTestEvent(t, db, profileID)
	insertTestEvent(t, db, profileID)

	appliedEvent := insertTestEventWithEndDate(t, db, otherProfileID, time.Now().Add(24*time.Hour))
	insertTestMember(t, db, appliedEvent, uuid.NullUUID{UUID: profileID, Valid: true})

	attendedEvent1 := insertTestEventWithEndDate(t, db, otherProfileID, time.Now().Add(-24*time.Hour))
	attendedEvent2 := insertTestEventWithEndDate(t, db, otherProfileID, time.Now().Add(-48*time.Hour))
	insertTestMember(t, db, attendedEvent1, uuid.NullUUID{UUID: profileID, Valid: true})
	insertTestMember(t, db, attendedEvent2, uuid.NullUUID{UUID: profileID, Valid: true})

	got, err := repo.CountMyEventKinds(context.Background(), profileID)
	if err != nil {
		t.Fatalf("CountMyEventKinds() returned error: %v", err)
	}

	want := model.MyEventCounts{Hosted: 2, Applied: 1, Attended: 2}
	if got != want {
		t.Errorf("CountMyEventKinds() = %#v, want %#v", got, want)
	}
}

// TestEventPostgres_ListMySummaries_EmptyReturnsEmptySlice は該当イベントが0件のとき
// nil ではなく空スライスを返すことを検証する。
func TestEventPostgres_ListMySummaries_EmptyReturnsEmptySlice(t *testing.T) {
	db := requireTestDB(t)
	repo := NewEventRepository(db)

	profileID := insertTestProfile(t, db)

	got, err := repo.ListMySummaries(context.Background(), model.MyEventFilter{
		ProfileID: profileID,
		Kind:      model.MyEventKindHosted,
	}, "created_at", "desc", 20, 0)
	if err != nil {
		t.Fatalf("ListMySummaries() returned error: %v", err)
	}
	if got == nil {
		t.Error("got = nil, want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}
