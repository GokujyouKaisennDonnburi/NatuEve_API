package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
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
	r, _, err := NewRouter(context.Background(), testConfig(), nil)
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
	r, _, err := NewRouter(context.Background(), testConfig(), nil)
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
			_, _, err := NewRouter(context.Background(), config.Config{CORSAllowOrigins: tt.origins}, nil)
			if err == nil {
				t.Fatal("NewRouter() error = nil, want error")
			}
		})
	}
}

// TestRegisterInternalRoutes_NotRegistered は、認証設定が揃っていない、または
// 通知送信ワーカーが無いときに内部エンドポイントが登録されず 404 になることを検証する（ADR-0035）。
func TestRegisterInternalRoutes_NotRegistered(t *testing.T) {
	worker := service.NewNotificationOutboxWorker(nil, nil, nil, nil)

	tests := []struct {
		name   string
		cfg    config.Config
		worker *service.NotificationOutboxWorker
	}{
		{name: "認証設定が両方未設定", cfg: config.Config{}, worker: worker},
		{name: "audience のみ設定", cfg: config.Config{SchedulerOIDCAudience: "https://api.example.com"}, worker: worker},
		{name: "サービスアカウントのみ設定", cfg: config.Config{SchedulerServiceAccountEmail: "s@p.iam.gserviceaccount.com"}, worker: worker},
		{
			name: "認証設定は揃っているがワーカーが無い（Resend 未設定）",
			cfg: config.Config{
				SchedulerOIDCAudience:        "https://api.example.com",
				SchedulerServiceAccountEmail: "s@p.iam.gserviceaccount.com",
			},
			worker: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			// 登録されないケースだけなので、JWKS の URL は使われない。
			if err := registerInternalRoutes(context.Background(), r, tt.cfg, tt.worker, ""); err != nil {
				t.Fatalf("registerInternalRoutes() returned error: %v", err)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/outbox/process", nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
			}
		})
	}
}

// TestRegisterInternalRoutes_RequiresSchedulerToken は、設定が揃っているとき内部エンドポイントが
// OIDC トークンの検証付きで登録されることを検証する（ADR-0035）。JWKS はテスト用のサーバーから配る。
func TestRegisterInternalRoutes_RequiresSchedulerToken(t *testing.T) {
	const (
		kid      = "test-scheduler-key"
		audience = "https://api.example.com"
		email    = "scheduler@project.iam.gserviceaccount.com"
	)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA 鍵生成に失敗: %v", err)
	}
	jwk, err := jwkset.NewJWKFromKey(key, jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{KID: kid, ALG: jwkset.AlgRS256, USE: jwkset.UseSig},
	})
	if err != nil {
		t.Fatalf("JWK 生成に失敗: %v", err)
	}
	storage := jwkset.NewMemoryStorage()
	if err := storage.KeyWrite(context.Background(), jwk); err != nil {
		t.Fatalf("JWK 登録に失敗: %v", err)
	}
	jwksJSON, err := storage.JSONPublic(context.Background())
	if err != nil {
		t.Fatalf("JWKS JSON 生成に失敗: %v", err)
	}
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	t.Cleanup(jwksServer.Close)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            audience,
		"email":          email,
		"email_verified": true,
		"exp":            time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("トークン署名に失敗: %v", err)
	}

	worker := service.NewNotificationOutboxWorker(emptyOutboxRepo{}, nil, nil, nil)
	cfg := config.Config{SchedulerOIDCAudience: audience, SchedulerServiceAccountEmail: email}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "トークンなしは 401", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "不正なトークンは 401", authHeader: "Bearer invalid", wantStatus: http.StatusUnauthorized},
		{name: "正しいトークンは 200", authHeader: "Bearer " + signed, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			if err := registerInternalRoutes(context.Background(), r, cfg, worker, jwksServer.URL); err != nil {
				t.Fatalf("registerInternalRoutes() returned error: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/internal/outbox/process", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// emptyOutboxRepo は処理対象の行が常に無い EventNotificationOutboxRepository のフェイク。
type emptyOutboxRepo struct{}

func (emptyOutboxRepo) ClaimNextDue(context.Context, time.Time, []uuid.UUID) (repository.OutboxClaim, error) {
	return nil, nil
}

const testRelaySecret = "new-secret-0123456789abcdef0123456789"

func TestNewRelayGuard(t *testing.T) {
	const header = "X-NatuEve-Client-IP"
	tests := []struct {
		name      string
		secret    string
		previous  string
		header    string
		wantGuard bool
		wantErr   bool
	}{
		{name: "全部未設定"},
		{name: "SECRET と HEADER が揃う", secret: testRelaySecret, header: header, wantGuard: true},
		{name: "SECRET だけ", secret: testRelaySecret, wantErr: true},
		{name: "HEADER だけ", header: header, wantErr: true},
		{name: "PREVIOUS だけ", previous: testRelaySecret, wantErr: true},
		{name: "3 つすべて", secret: testRelaySecret, previous: "old-secret-0123456789abcdef0123456789", header: header, wantGuard: true},
		{name: "SECRET が 31 バイト", secret: strings.Repeat("a", 31), header: header, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, err := newRelayGuard(config.Config{
				RelaySecret:         tt.secret,
				RelaySecretPrevious: tt.previous,
				RelayClientIPHeader: tt.header,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("newRelayGuard() error = %v, wantErr %v", err, tt.wantErr)
			}
			if (guard != nil) != tt.wantGuard {
				t.Errorf("newRelayGuard() guard non-nil = %v, want %v", guard != nil, tt.wantGuard)
			}
		})
	}
}

// TestNewRouterRelayGuard は、RELAY_* を設定した NewRouter が中継元を確認することを検証する（ADR-0034）。
func TestNewRouterRelayGuard(t *testing.T) {
	const (
		header = "X-NatuEve-Client-IP"
		origin = "http://localhost:3000"
	)
	cfg := testConfig()
	cfg.RelaySecret = testRelaySecret
	cfg.RelayClientIPHeader = header

	r, _, err := NewRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() returned error: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		secret     string
		preflight  bool
		wantStatus int
		wantCORS   bool
	}{
		{name: "/health は秘密ヘッダーなしで 200", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
		{name: "末尾スラッシュ違いの /health/ は秘密ヘッダーなしで 403", method: http.MethodGet, path: "/health/", wantStatus: http.StatusForbidden},
		{name: "末尾スラッシュ違いの /health/ は正しい値で 404", method: http.MethodGet, path: "/health/", secret: testRelaySecret, wantStatus: http.StatusNotFound},
		{name: "/swagger は秘密ヘッダーなしで 403", method: http.MethodGet, path: "/swagger/index.html", wantStatus: http.StatusForbidden},
		{name: "/swagger は正しい値で 200", method: http.MethodGet, path: "/swagger/index.html", secret: testRelaySecret, wantStatus: http.StatusOK},
		{name: "未登録のパスは秘密ヘッダーなしで 403", method: http.MethodGet, path: "/no-such-path", wantStatus: http.StatusForbidden},
		{name: "未登録のパスは正しい値で 404", method: http.MethodGet, path: "/no-such-path", secret: testRelaySecret, wantStatus: http.StatusNotFound},
		{name: "プリフライトは秘密ヘッダーなしで 403", method: http.MethodOptions, path: "/swagger/index.html", preflight: true, wantStatus: http.StatusForbidden},
		{name: "プリフライトは正しい値で 204", method: http.MethodOptions, path: "/swagger/index.html", secret: testRelaySecret, preflight: true, wantStatus: http.StatusNoContent, wantCORS: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.secret != "" {
				req.Header.Set("X-Relay-Auth", tt.secret)
			}
			if tt.preflight {
				req.Header.Set("Origin", origin)
				req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			got := w.Header().Get("Access-Control-Allow-Origin")
			if tt.wantCORS && got != origin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
			}
			if !tt.wantCORS && got != "" {
				t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
			}
		})
	}

	if r.TrustedPlatform != header {
		t.Errorf("TrustedPlatform = %q, want %q", r.TrustedPlatform, header)
	}

	disabled, _, err := NewRouter(context.Background(), testConfig(), nil)
	if err != nil {
		t.Fatalf("NewRouter() returned error: %v", err)
	}
	if disabled.TrustedPlatform != "" {
		t.Errorf("TrustedPlatform = %q, want empty", disabled.TrustedPlatform)
	}
}
