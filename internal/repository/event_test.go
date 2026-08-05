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

// TestNormalizeSearchText は normalizeSearchText(NFKC) が半角/全角の表記ゆれを
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
			if got := normalizeSearchText(tt.input); got != tt.want {
				t.Errorf("normalizeSearchText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBuildSearchWhere は buildSearchWhere が各キーワードを5フィールド OR の
// 1グループとし、グループ間を AND で連結すること、タグ条件を EXISTS + IN の1グループに
// まとめること、プレースホルダを startIdx から連番で割り当てること（キーワード→タグの順）、
// ILIKE パターン・タグID引数を順序どおり生成することを検証する。
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildSearchWhere(tt.filter, tt.startIdx)

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
