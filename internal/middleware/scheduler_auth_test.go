package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testSchedulerAudience = "https://api.example.com"
	testSchedulerEmail    = "scheduler@project.iam.gserviceaccount.com"
)

// schedulerClaims は Cloud Scheduler が付ける OIDC トークンの正常系クレームを返す。
func schedulerClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testSchedulerAudience,
		"email":          testSchedulerEmail,
		"email_verified": true,
		"exp":            time.Now().UTC().Add(time.Hour).Unix(),
	}
}

// withClaims は schedulerClaims の一部を上書き（値が nil ならキーを削除）したクレームを返す。
func withClaims(overrides map[string]any) jwt.MapClaims {
	claims := schedulerClaims()
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

func TestRequireScheduler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base, key := newTestVerifier(t)
	verifier := &SchedulerVerifier{keyfunc: base.keyfunc, audience: testSchedulerAudience, email: testSchedulerEmail}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("別 RSA 鍵生成に失敗: %v", err)
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "正常: 正しい OIDC トークン",
			authHeader: "Bearer " + signToken(t, key, schedulerClaims()),
			wantStatus: http.StatusOK,
		},
		{
			name:       "正常: iss が accounts.google.com（スキームなし）",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"iss": "accounts.google.com"})),
			wantStatus: http.StatusOK,
		},
		{
			name:       "異常: Authorization ヘッダなし",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: 署名が不正(別の鍵で署名)",
			authHeader: "Bearer " + signToken(t, otherKey, schedulerClaims()),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: 期限切れ",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"exp": time.Now().UTC().Add(-time.Hour).Unix()})),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: 有効期限(exp)なし",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"exp": nil})),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: aud 不一致",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"aud": "https://other.example.com"})),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: iss が Google ではない",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"iss": "https://evil.example.com"})),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: サービスアカウントのメールアドレスが不一致",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email": "other@project.iam.gserviceaccount.com"})),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "異常: email がない",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email": nil})),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "異常: email_verified が false",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email_verified": false})),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "正常: email_verified が文字列の \"true\"",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email_verified": "true"})),
			wantStatus: http.StatusOK,
		},
		{
			name:       "異常: email_verified が文字列の \"false\"",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email_verified": "false"})),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "異常: email_verified がない",
			authHeader: "Bearer " + signToken(t, key, withClaims(map[string]any{"email_verified": nil})),
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/internal/outbox/process", verifier.RequireScheduler(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

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

// TestRequireScheduler_RejectsOtherAlgorithms は RS256 以外の署名方式（none、HMAC）を拒否することを検証する。
func TestRequireScheduler_RejectsOtherAlgorithms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base, _ := newTestVerifier(t)
	verifier := &SchedulerVerifier{keyfunc: base.keyfunc, audience: testSchedulerAudience, email: testSchedulerEmail}

	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, schedulerClaims()).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("alg=none トークン生成に失敗: %v", err)
	}
	hmacToken := jwt.NewWithClaims(jwt.SigningMethodHS256, schedulerClaims())
	hmacToken.Header["kid"] = testKID
	hmacSigned, err := hmacToken.SignedString([]byte("shared-secret"))
	if err != nil {
		t.Fatalf("HS256 トークン生成に失敗: %v", err)
	}

	for name, token := range map[string]string{"alg=none": noneToken, "HS256": hmacSigned} {
		t.Run(name, func(t *testing.T) {
			r := gin.New()
			r.POST("/internal/outbox/process", verifier.RequireScheduler(), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/internal/outbox/process", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}
