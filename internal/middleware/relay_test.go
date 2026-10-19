package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const (
	testRelaySecret         = "new-secret-0123456789abcdef0123456789"
	testRelaySecretPrevious = "old-secret-0123456789abcdef0123456789"
	testRelayIPHeader       = "X-NatuEve-Client-IP"
	testRelayRemoteAddr     = "192.0.2.10:12345"
	testRelayRemoteIP       = "192.0.2.10"
)

var testRelayExemptPaths = []string{"/health", "/internal/outbox/process"}

func TestNewRelayGuardValidation(t *testing.T) {
	tests := []struct {
		name    string
		secrets []string
		header  string
		wantErr bool
	}{
		{name: "値が 1 つ", secrets: []string{testRelaySecret}, header: testRelayIPHeader},
		{name: "値が 2 つ", secrets: []string{testRelaySecret, testRelaySecretPrevious}, header: testRelayIPHeader},
		{name: "ちょうど 32 バイト", secrets: []string{strings.Repeat("a", 32)}, header: testRelayIPHeader},

		{name: "secrets が nil", secrets: nil, header: testRelayIPHeader, wantErr: true},
		{name: "secrets が空", secrets: []string{}, header: testRelayIPHeader, wantErr: true},
		{name: "31 バイトの値", secrets: []string{strings.Repeat("a", 31)}, header: testRelayIPHeader, wantErr: true},
		{name: "2 つ目だけが短い", secrets: []string{testRelaySecret, "short"}, header: testRelayIPHeader, wantErr: true},
		{name: "clientIPHeader が空", secrets: []string{testRelaySecret}, header: "", wantErr: true},
		{name: "clientIPHeader が RelayAuthHeader の大文字小文字違い", secrets: []string{testRelaySecret}, header: "x-relay-auth", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, err := NewRelayGuard(tt.secrets, tt.header, testRelayExemptPaths)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewRelayGuard() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && guard == nil {
				t.Fatal("NewRelayGuard() = nil, want non-nil")
			}
		})
	}
}

// relayProbe は guard の後ろのハンドラが観測した値。
type relayProbe struct {
	ClientIP  string `json:"client_ip"`
	RelayAuth string `json:"relay_auth"`
	IPHeader  string `json:"ip_header"`
}

// newRelayTestEngine は guard の後ろに relayProbe を返すハンドラを置いたエンジンと、
// ハンドラが呼ばれた回数を返す関数を返す。
func newRelayTestEngine(t *testing.T, pre ...gin.HandlerFunc) (*gin.Engine, func() int) {
	t.Helper()
	guard, err := NewRelayGuard([]string{testRelaySecret, testRelaySecretPrevious}, testRelayIPHeader, testRelayExemptPaths)
	if err != nil {
		t.Fatalf("NewRelayGuard() returned error: %v", err)
	}

	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies() returned error: %v", err)
	}
	r.TrustedPlatform = testRelayIPHeader

	calls := 0
	r.Use(pre...)
	r.Use(guard)
	r.Any("/*path", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, relayProbe{
			ClientIP:  c.ClientIP(),
			RelayAuth: c.GetHeader(RelayAuthHeader),
			IPHeader:  c.GetHeader(testRelayIPHeader),
		})
	})
	return r, func() int { return calls }
}

func TestRelayGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const spoofedIP = "203.0.113.99"
	const relayedIP = "198.51.100.7"

	tests := []struct {
		name       string
		method     string
		path       string
		secret     *string // nil ならヘッダーを付けない
		wantStatus int
		wantProbe  *relayProbe // 200 のときだけ検証する
	}{
		{
			name: "新しい値で一致", method: http.MethodGet, path: "/events", secret: ptr(testRelaySecret),
			wantStatus: http.StatusOK, wantProbe: &relayProbe{ClientIP: relayedIP, RelayAuth: "", IPHeader: relayedIP},
		},
		{
			name: "古い値で一致", method: http.MethodGet, path: "/events", secret: ptr(testRelaySecretPrevious),
			wantStatus: http.StatusOK, wantProbe: &relayProbe{ClientIP: relayedIP, RelayAuth: "", IPHeader: relayedIP},
		},
		{name: "秘密ヘッダーなし", method: http.MethodGet, path: "/events", secret: nil, wantStatus: http.StatusForbidden},
		{name: "空文字", method: http.MethodGet, path: "/events", secret: ptr(""), wantStatus: http.StatusForbidden},
		{name: "違う値", method: http.MethodGet, path: "/events", secret: ptr("wrong-secret-0123456789abcdef012345"), wantStatus: http.StatusForbidden},
		{name: "正しい値より 1 文字短い", method: http.MethodGet, path: "/events", secret: ptr(testRelaySecret[:len(testRelaySecret)-1]), wantStatus: http.StatusForbidden},
		{name: "正しい値より 1 文字長い", method: http.MethodGet, path: "/events", secret: ptr(testRelaySecret + "x"), wantStatus: http.StatusForbidden},
		{
			name: "除外パスで秘密ヘッダーなし（IP ヘッダーは偽装）", method: http.MethodGet, path: "/health", secret: nil,
			wantStatus: http.StatusOK, wantProbe: &relayProbe{ClientIP: testRelayRemoteIP, RelayAuth: "", IPHeader: ""},
		},
		{
			name: "除外パスで誤った値（IP ヘッダーは偽装）", method: http.MethodPost, path: "/internal/outbox/process", secret: ptr("wrong"),
			wantStatus: http.StatusOK, wantProbe: &relayProbe{ClientIP: testRelayRemoteIP, RelayAuth: "", IPHeader: ""},
		},
		{
			name: "除外パスで正しい値", method: http.MethodGet, path: "/health", secret: ptr(testRelaySecret),
			wantStatus: http.StatusOK, wantProbe: &relayProbe{ClientIP: relayedIP, RelayAuth: "", IPHeader: relayedIP},
		},
		{name: "除外パスに似たパス /health/", method: http.MethodGet, path: "/health/", secret: nil, wantStatus: http.StatusForbidden},
		{name: "除外パスに似たパス /healthz", method: http.MethodGet, path: "/healthz", secret: nil, wantStatus: http.StatusForbidden},
		{name: "除外パスに似たパス /internal/outbox/process/x", method: http.MethodPost, path: "/internal/outbox/process/x", secret: nil, wantStatus: http.StatusForbidden},
		{name: "除外でないパスへの OPTIONS", method: http.MethodOptions, path: "/events", secret: nil, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, calls := newRelayTestEngine(t)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.RemoteAddr = testRelayRemoteAddr
			req.Header.Set(testRelayIPHeader, spoofedIP)
			if tt.secret != nil {
				req.Header.Set(RelayAuthHeader, *tt.secret)
			}
			// 秘密ヘッダーが正しい場合は Worker が入れた IP になる。
			if tt.secret != nil && (*tt.secret == testRelaySecret || *tt.secret == testRelaySecretPrevious) {
				req.Header.Set(testRelayIPHeader, relayedIP)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusForbidden {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatalf("failed to parse body %q: %v", w.Body.String(), err)
				}
				if body.Error.Code != "forbidden" {
					t.Errorf("error.code = %q, want %q", body.Error.Code, "forbidden")
				}
				if n := calls(); n != 0 {
					t.Errorf("ハンドラが %d 回呼ばれた, want 0", n)
				}
				return
			}

			var got relayProbe
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse body %q: %v", w.Body.String(), err)
			}
			if got != *tt.wantProbe {
				t.Errorf("probe = %+v, want %+v", got, *tt.wantProbe)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// TestRelayGuardRejectLogOmitsSecrets は、拒否時のログに偽装された IP と送られた秘密値が
// 含まれず、接続元の IP が含まれることを検証する。
func TestRelayGuardRejectLogOmitsSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	const spoofedIP = "203.0.113.99"
	const wrongSecret = "wrong-secret-0123456789abcdef012345"

	r, _ := newRelayTestEngine(t, SlogLogger())
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.RemoteAddr = testRelayRemoteAddr
	req.Header.Set(testRelayIPHeader, spoofedIP)
	req.Header.Set(RelayAuthHeader, wrongSecret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	logs := buf.String()
	if logs == "" {
		t.Fatal("ログが出力されていない")
	}
	for _, s := range []string{spoofedIP, wrongSecret} {
		if strings.Contains(logs, s) {
			t.Errorf("ログに %q が含まれている:\n%s", s, logs)
		}
	}
	if !strings.Contains(logs, testRelayRemoteIP) {
		t.Errorf("ログに接続元の IP %q が含まれていない:\n%s", testRelayRemoteIP, logs)
	}
}
