package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

func TestContactRouteGating(t *testing.T) {
	tests := []struct {
		name, key, from, to string
		noDB                bool
		status              int
	}{
		{"設定済み・JWKSなし", "test-key", "from@example.com", "admin@example.com", false, 400},
		{"CONTACT_TOなし", "test-key", "from@example.com", "", false, 404},
		{"CONTACT_TO空白", "test-key", "from@example.com", "  ", false, 404},
		{"RESEND_API_KEYなし", "", "from@example.com", "admin@example.com", false, 404},
		{"MAIL_FROMなし", "test-key", "", "admin@example.com", false, 404},
		{"DBなし", "test-key", "from@example.com", "admin@example.com", true, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.ResendAPIKey, cfg.MailFrom, cfg.ContactTo = tt.key, tt.from, tt.to
			db := newFailingDB(t)
			if tt.noDB {
				db = nil
			}
			r, _, err := NewRouter(context.Background(), cfg, db)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			// 不正入力はDBやResendへ到達せず、ルートの登録有無だけ確認できる。
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/contact", strings.NewReader(`{}`)))
			if w.Code != tt.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestContactToValidation(t *testing.T) {
	for _, value := range []string{"not-an-email", "a@example.com,b@example.com", "Admin <a@example.com>", "a@example.com\r\nBcc: b@example.com"} {
		t.Run(value, func(t *testing.T) {
			cfg := testConfig()
			cfg.ContactTo = value
			_, _, err := NewRouter(context.Background(), cfg, nil)
			if err == nil || !strings.Contains(err.Error(), "CONTACT_TO") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestContactRateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.ResendAPIKey, cfg.MailFrom, cfg.ContactTo = "test-key", "from@example.com", "admin@example.com"
	r, _, err := NewRouter(context.Background(), cfg, newFailingDB(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contact", strings.NewReader(`{}`))
		req.RemoteAddr = "203.0.113.20:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := http.StatusBadRequest
		if i == 5 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("request %d: status=%d want=%d", i+1, w.Code, want)
		}
		if i == 5 {
			var resp model.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Error.Code != "rate_limited" {
				t.Fatalf("error=%+v", resp)
			}
		}
	}
	// 別IPとゲスト申込にはお問い合わせの消費枠を引き継がない。
	for _, test := range []struct{ ip, path string }{
		{"203.0.113.21:1234", "/api/v1/contact"},
		{"203.0.113.20:1234", "/api/v1/events/invalid/join"},
	} {
		req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{}`))
		req.RemoteAddr = test.ip
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", test.path, w.Code)
		}
	}
}
