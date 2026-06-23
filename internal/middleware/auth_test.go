package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKID      = "test-key-1"
	testAudience = "authenticated"
)

// newTestVerifier はテスト用に、生成した RSA 公開鍵を含む JWKS から検証器を構築する。
// 署名に使う秘密鍵もあわせて返す。
func newTestVerifier(t *testing.T) (*SupabaseVerifier, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA 鍵生成に失敗: %v", err)
	}

	jwk, err := jwkset.NewJWKFromKey(key, jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{
			KID: testKID,
			ALG: jwkset.AlgRS256,
			USE: jwkset.UseSig,
		},
	})
	if err != nil {
		t.Fatalf("JWK 生成に失敗: %v", err)
	}

	storage := jwkset.NewMemoryStorage()
	if err := storage.KeyWrite(context.Background(), jwk); err != nil {
		t.Fatalf("JWK 登録に失敗: %v", err)
	}
	raw, err := storage.JSONPublic(context.Background())
	if err != nil {
		t.Fatalf("JWKS JSON 生成に失敗: %v", err)
	}

	kf, err := keyfunc.NewJWKSetJSON(raw)
	if err != nil {
		t.Fatalf("keyfunc 構築に失敗: %v", err)
	}
	return &SupabaseVerifier{keyfunc: kf, audience: testAudience}, key
}

// signToken は与えたクレームを kid 付き RS256 で署名する。
func signToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKID
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("トークン署名に失敗: %v", err)
	}
	return signed
}

// validClaims は正常系の標準クレームを返す。
func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":   "d290f1ee-6c54-4b01-90e6-d701748f0851",
		"email": "user@example.com",
		"aud":   testAudience,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"user_metadata": map[string]any{
			"full_name":  "なちゅいべ太郎",
			"avatar_url": "https://example.com/a.png",
		},
	}
}

func TestRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	verifier, key := newTestVerifier(t)
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
			name:       "正常: 有効なトークン",
			authHeader: "Bearer " + signToken(t, key, validClaims()),
			wantStatus: http.StatusOK,
		},
		{
			name: "異常: 期限切れ",
			authHeader: "Bearer " + signToken(t, key, jwt.MapClaims{
				"sub": "abc", "aud": testAudience,
				"exp": time.Now().Add(-time.Hour).Unix(),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: 署名が不正(別の鍵で署名)",
			authHeader: "Bearer " + signToken(t, otherKey, validClaims()),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "異常: Authorization ヘッダなし",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "異常: aud 不一致",
			authHeader: "Bearer " + signToken(t, key, jwt.MapClaims{
				"sub": "abc", "aud": "other",
				"exp": time.Now().Add(time.Hour).Unix(),
			}),
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUser AuthUser
			var gotOK bool

			r := gin.New()
			r.Use(verifier.RequireAuth())
			r.GET("/me", func(c *gin.Context) {
				gotUser, gotOK = AuthFromContext(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				if !gotOK {
					t.Fatalf("AuthFromContext が取得できなかった")
				}
				if gotUser.ID != "d290f1ee-6c54-4b01-90e6-d701748f0851" {
					t.Errorf("ID = %q", gotUser.ID)
				}
				if gotUser.Email != "user@example.com" {
					t.Errorf("Email = %q", gotUser.Email)
				}
				if gotUser.DisplayName != "なちゅいべ太郎" {
					t.Errorf("DisplayName = %q", gotUser.DisplayName)
				}
				if gotUser.AvatarURL != "https://example.com/a.png" {
					t.Errorf("AvatarURL = %q", gotUser.AvatarURL)
				}
			}
		})
	}
}
