package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewCORSValidation(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		wantErr bool
	}{
		{name: "localhost", origins: []string{"http://localhost:3000"}},
		{name: "https ドメイン", origins: []string{"https://example.com"}},
		{name: "複数指定", origins: []string{"http://localhost:3000", "https://example.com"}},
		{name: "IPv6", origins: []string{"http://[::1]:3000"}},
		{name: "大文字を含むホスト", origins: []string{"https://Example.com"}},
		{name: "大文字のスキーム", origins: []string{"HTTP://example.com"}},

		{name: "nil", origins: nil, wantErr: true},
		{name: "空スライス", origins: []string{}, wantErr: true},
		{name: "ワイルドカード", origins: []string{"*"}, wantErr: true},
		{name: "サブドメインワイルドカード", origins: []string{"https://*.example.com"}, wantErr: true},
		{name: "正規表現", origins: []string{"/^https://.*$/"}, wantErr: true},
		{name: "末尾スラッシュ", origins: []string{"https://example.com/"}, wantErr: true},
		{name: "パス付き", origins: []string{"https://example.com/path"}, wantErr: true},
		{name: "クエリ付き", origins: []string{"https://example.com?x=1"}, wantErr: true},
		{name: "フラグメント付き", origins: []string{"https://example.com#f"}, wantErr: true},
		{name: "ユーザー情報付き", origins: []string{"https://user@example.com"}, wantErr: true},
		{name: "http/https 以外のスキーム", origins: []string{"ftp://example.com"}, wantErr: true},
		{name: "スキームなし", origins: []string{"example.com"}, wantErr: true},
		{name: "ホストなし", origins: []string{"http://"}, wantErr: true},
		{name: "ポートのみ", origins: []string{"http://:3000"}, wantErr: true},
		{name: "空のポート", origins: []string{"https://example.com:"}, wantErr: true},
		{name: "IPv6 の空のポート", origins: []string{"http://[::1]:"}, wantErr: true},
		{name: "正常値に不正値が混在", origins: []string{"https://example.com", "*"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewCORS(tt.origins)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewCORS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && h == nil {
				t.Error("NewCORS() handler = nil, want non-nil")
			}
		})
	}
}

// newCORSRouter は指定オリジンを許可する CORS 付きのテスト用ルーターを組み立てるヘルパー。
func newCORSRouter(t *testing.T, origins ...string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, err := NewCORS(origins)
	if err != nil {
		t.Fatalf("NewCORS() returned error: %v", err)
	}
	r := gin.New()
	r.Use(h)
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

// doCORSRequest は Origin 付きでリクエストを送り、レスポンスを返すヘルパー。
// preflight が true なら Access-Control-Request-Method 付きの OPTIONS を送る。
func doCORSRequest(t *testing.T, r *gin.Engine, origin string, preflight bool) *httptest.ResponseRecorder {
	t.Helper()
	method := http.MethodGet
	if preflight {
		method = http.MethodOptions
	}
	req := httptest.NewRequest(method, "/test", nil)
	// httptest.NewRequest の既定 Host は example.com で、Origin と同一ホストだと
	// gin-contrib/cors が同一オリジン扱いにして CORS ヘッダを付けないため、別ホストにする。
	req.Host = "api.test"
	req.Header.Set("Origin", origin)
	if preflight {
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORSRequests(t *testing.T) {
	const (
		first  = "http://localhost:3000"
		second = "https://example.com"
	)

	tests := []struct {
		name          string
		allowed       []string
		origin        string
		preflight     bool
		wantStatus    int
		wantOrigin    string // 期待する Access-Control-Allow-Origin（空なら付かない）
		wantMethods   bool   // Access-Control-Allow-Methods が付くこと
		wantCredsTrue bool
	}{
		{
			name:          "許可オリジンの GET に Allow-Origin と Allow-Credentials が付く",
			allowed:       []string{first},
			origin:        first,
			wantStatus:    http.StatusOK,
			wantOrigin:    first,
			wantCredsTrue: true,
		},
		{
			name:       "許可していないオリジンは 403",
			allowed:    []string{first},
			origin:     "https://evil.example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:          "許可オリジンのプリフライトは 204 で Allow-Methods を含む",
			allowed:       []string{first},
			origin:        first,
			preflight:     true,
			wantStatus:    http.StatusNoContent,
			wantOrigin:    first,
			wantMethods:   true,
			wantCredsTrue: true,
		},
		{
			name:          "複数指定時に 2 つ目のオリジンも許可される",
			allowed:       []string{first, second},
			origin:        second,
			wantStatus:    http.StatusOK,
			wantOrigin:    second,
			wantCredsTrue: true,
		},
		{
			name:          "設定値の大文字（スキーム・ホスト）は小文字の Origin と一致する",
			allowed:       []string{"HTTPS://Example.com"},
			origin:        second,
			wantStatus:    http.StatusOK,
			wantOrigin:    second,
			wantCredsTrue: true,
		},
		{
			name:       "完全一致のみ: ポート違いは 403",
			allowed:    []string{first},
			origin:     "http://localhost:3001",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCORSRouter(t, tt.allowed...)
			w := doCORSRequest(t, r, tt.origin, tt.preflight)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantOrigin)
			}
			if tt.wantMethods && w.Header().Get("Access-Control-Allow-Methods") == "" {
				t.Error("Access-Control-Allow-Methods is empty, want non-empty")
			}
			if tt.wantCredsTrue {
				if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
					t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
				}
			}
		})
	}
}
