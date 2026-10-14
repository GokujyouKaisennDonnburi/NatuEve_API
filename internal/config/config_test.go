package config

import (
	"os"
	"reflect"
	"testing"
)

func TestLoadCORSAllowOrigins(t *testing.T) {
	tests := []struct {
		name  string
		set   bool // false なら CORS_ALLOW_ORIGINS を未設定にする
		value string
		want  []string
	}{
		{name: "未設定なら既定値", set: false, want: []string{"http://localhost:3000"}},
		{name: "空文字なら既定値", set: true, value: "", want: []string{"http://localhost:3000"}},
		{name: "単一", set: true, value: "https://example.com", want: []string{"https://example.com"}},
		{
			name: "複数", set: true, value: "http://localhost:3000,https://example.com",
			want: []string{"http://localhost:3000", "https://example.com"},
		},
		{
			name: "前後の空白を除く", set: true, value: " https://a.example.com ,\thttps://b.example.com  ",
			want: []string{"https://a.example.com", "https://b.example.com"},
		},
		{name: "空要素を除く", set: true, value: "a, ,b,", want: []string{"a", "b"}},
		{name: "空要素のみなら空スライス", set: true, value: " , ", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv で元の値の復元を登録してから、未設定ケースは Unsetenv する。
			t.Setenv("CORS_ALLOW_ORIGINS", tt.value)
			if !tt.set {
				if err := os.Unsetenv("CORS_ALLOW_ORIGINS"); err != nil {
					t.Fatalf("Unsetenv failed: %v", err)
				}
			}

			got := Load().CORSAllowOrigins
			if len(got) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("CORSAllowOrigins = %#v, want %#v", got, tt.want)
			}
		})
	}
}
