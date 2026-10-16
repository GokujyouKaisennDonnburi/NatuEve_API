package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
)

func TestMain(m *testing.M) {
	// テスト中はデバッグ出力を抑える。
	gin.SetMode(gin.TestMode)
	m.Run()
}

// testConfig は CORS の許可オリジンだけを設定したテスト用 Config を返す。
func testConfig() config.Config {
	return config.Config{CORSAllowOrigins: []string{"http://localhost:3000"}}
}

func TestHealthEndpoint(t *testing.T) {
	r, _, err := NewRouter(testConfig(), nil)
	if err != nil {
		t.Fatalf("NewRouter() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body %q: %v", w.Body.String(), err)
	}

	if got := body["status"]; got != "ok" {
		t.Errorf("status field = %q, want %q", got, "ok")
	}
}

// TestQueryValidationRunsAfterCORS は、ValidateQuery() を NewCORS() より後ろに
// 積むという設計判断（router.go 参照）を検証する統合テスト。
//
// ValidateQuery() が CORS より前に置かれてしまうと、不正なクエリでの 400 レスポンスに
// Access-Control-Allow-Origin ヘッダが付かず、ブラウザからは 400 ではなく CORS エラー
// として観測されて原因が追えなくなる。ミドルウェアの順序が将来のリファクタで
// 入れ替わった場合にこのテストが落ちることを確認済み。
func TestQueryValidationRunsAfterCORS(t *testing.T) {
	r, _, err := NewRouter(testConfig(), nil)
	if err != nil {
		t.Fatalf("NewRouter() returned error: %v", err)
	}

	// testConfig() の CORSAllowOrigins に含まれる Origin。
	const allowedOrigin = "http://localhost:3000"

	req := httptest.NewRequest(http.MethodGet, "/health?q=%zz", nil)
	req.Header.Set("Origin", allowedOrigin)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("Access-Control-Allow-Origin header = %q, want %q", got, allowedOrigin)
	}
}

// TestNewRouterRejectsInvalidCORSOrigins は、CORSAllowOrigins が空または不正なとき
// NewRouter がエラーを返すことを検証する（ADR-0033）。
func TestNewRouterRejectsInvalidCORSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
	}{
		{name: "nil", origins: nil},
		{name: "空スライス", origins: []string{}},
		{name: "ワイルドカード", origins: []string{"*"}},
		{name: "末尾スラッシュ付き", origins: []string{"https://example.com/"}},
		{name: "正常値に不正値が混在", origins: []string{"http://localhost:3000", "ftp://example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := NewRouter(config.Config{CORSAllowOrigins: tt.origins}, nil)
			if err == nil {
				t.Fatal("NewRouter() error = nil, want error")
			}
		})
	}
}
