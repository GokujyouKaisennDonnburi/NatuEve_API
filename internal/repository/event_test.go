package repository

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

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
// 1グループとし、グループ間を AND で連結すること、プレースホルダを startIdx から
// 連番で割り当てること、ILIKE パターン引数を順序どおり生成することを検証する。
func TestBuildSearchWhere(t *testing.T) {
	t.Helper()

	tests := []struct {
		name     string
		keywords []string
		startIdx int
		// wantContains は生成 WHERE 句に必ず含まれるべき部分文字列。
		wantContains []string
		// wantNotContains は含まれてはならない部分文字列（AND 連結の確認等）。
		wantNotContains []string
		wantAndCount    int // " AND " の出現回数（グループ数-1）
		wantArgs        []any
	}{
		{
			name:     "単一キーワード: $1 が5フィールド(normalize適用)へ展開され AND を含まない",
			keywords: []string{"桜"},
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
			keywords:        []string{"桜", "東京"},
			startIdx:        1,
			wantContains:    []string{"ILIKE $1", "ILIKE $2", ") AND ("},
			wantNotContains: []string{"ILIKE $3"},
			wantAndCount:    1,
			wantArgs:        []any{"%桜%", "%東京%"},
		},
		{
			name:         "startIdx オフセット: limit/offset を後続に置くため $3 から開始",
			keywords:     []string{"a", "b"},
			startIdx:     3,
			wantContains: []string{"ILIKE $3", "ILIKE $4"},
			wantAndCount: 1,
			wantArgs:     []any{"%a%", "%b%"},
		},
		{
			name:         "特殊文字を含むキーワードはエスケープされてパターン化される",
			keywords:     []string{"50%"},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{`%50\%%`},
		},
		{
			name:         "全角数字は NFKC 正規化で半角化されてパターン化される",
			keywords:     []string{"２０２６"},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{"%2026%"},
		},
		{
			name:         "全角パーセントは NFKC で ASCII 化された後 LIKE エスケープされる",
			keywords:     []string{"５０％"},
			startIdx:     1,
			wantContains: []string{"ILIKE $1"},
			wantArgs:     []any{`%50\%%`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildSearchWhere(tt.keywords, tt.startIdx)

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
