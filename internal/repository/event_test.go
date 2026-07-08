package repository

import (
	"reflect"
	"strings"
	"testing"
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
