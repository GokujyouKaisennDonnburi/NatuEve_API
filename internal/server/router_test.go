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

func TestHealthEndpoint(t *testing.T) {
	r, _, err := NewRouter(config.Config{}, nil)
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
	r, _, err := NewRouter(config.Config{}, nil)
	if err != nil {
		t.Fatalf("NewRouter() returned error: %v", err)
	}

	// NewCORS() が AllowOrigins に許可している Origin（internal/middleware/cors.go 参照）。
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
