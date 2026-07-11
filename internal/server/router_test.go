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
