package service

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "通常のASCII名はそのまま", in: "report.pdf", want: "report.pdf"},
		{name: "日本語名はそのまま", in: "報告書.pdf", want: "報告書.pdf"},
		{name: "スラッシュはbasenameのみ", in: "a/b/c.pdf", want: "c.pdf"},
		{name: "バックスラッシュはbasenameのみ", in: `a\b\c.pdf`, want: "c.pdf"},
		{name: "パストラバーサルはbasenameのみ", in: "../../etc/passwd", want: "passwd"},
		{name: "制御文字(CR/LF/タブ)は除去", in: "a\r\nb\tc.pdf", want: "abc.pdf"},
		{name: "前後空白はトリム", in: "  spaced.pdf  ", want: "spaced.pdf"},
		{name: "空文字は空文字", in: "", want: ""},
		{name: "空白のみは空文字", in: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilenameLength(t *testing.T) {
	long := make([]rune, maxFilenameLen+50)
	for i := range long {
		long[i] = 'a'
	}
	got := sanitizeFilename(string(long))
	if len([]rune(got)) != maxFilenameLen {
		t.Errorf("長い名前が %d rune に切り詰められていない: got %d", maxFilenameLen, len([]rune(got)))
	}
}

func TestContentDispositionInline(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ASCII名",
			in:   "report.pdf",
			want: `inline; filename="report.pdf"; filename*=UTF-8''report.pdf`,
		},
		{
			name: "日本語名はfilename*でパーセントエンコード・ASCIIはフォールバック",
			in:   "報告書.pdf",
			want: `inline; filename="___.pdf"; filename*=UTF-8''%E5%A0%B1%E5%91%8A%E6%9B%B8.pdf`,
		},
		{
			name: "ダブルクオートはエスケープ/パーセントエンコード",
			in:   `a"b.pdf`,
			want: `inline; filename="a\"b.pdf"; filename*=UTF-8''a%22b.pdf`,
		},
		{
			name: "空名はfallback(download)",
			in:   "",
			want: `inline; filename="download"; filename*=UTF-8''download`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contentDispositionInline(tt.in); got != tt.want {
				t.Errorf("contentDispositionInline(%q) =\n  got  %q\n  want %q", tt.in, got, tt.want)
			}
		})
	}
}
