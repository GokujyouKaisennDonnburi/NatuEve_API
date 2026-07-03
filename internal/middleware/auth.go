package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// authUserContextKey は gin.Context に AuthUser を格納する際のキー。
const authUserContextKey = "authUser"

// AuthUser は検証済み JWT から取り出した認証ユーザー情報。
type AuthUser struct {
	// ID は Supabase Auth のユーザー ID(JWT の sub)。
	ID string
	// Email はユーザーのメールアドレス。
	Email string
	// DisplayName は表示名(Google プロフィール由来。無ければ空)。
	DisplayName string
	// AvatarURL はアバター画像 URL(Google プロフィール由来。無ければ空)。
	AvatarURL string
	// Description は自己紹介(Google プロフィール由来。無ければ空)。
	Description string
}

// SupabaseVerifier は Supabase Auth が発行した JWT を JWKS で検証する。
//
// 署名鍵は JWKS エンドポイントから取得し、バックグラウンドで更新される。
type SupabaseVerifier struct {
	keyfunc  keyfunc.Keyfunc
	audience string
}

// NewSupabaseVerifier は設定の JWKS URL から検証器を構築する。
//
// JWKS の取得・更新は keyfunc がバックグラウンドで行う。
func NewSupabaseVerifier(cfg config.Config) (*SupabaseVerifier, error) {
	kf, err := keyfunc.NewDefaultCtx(context.Background(), []string{cfg.SupabaseJWKSURL})
	if err != nil {
		return nil, err
	}
	return &SupabaseVerifier{keyfunc: kf, audience: cfg.SupabaseJWTAudience}, nil
}

// RequireAuth は Authorization ヘッダの Bearer トークンを検証する gin ミドルウェアを返す。
//
// 検証に成功すると AuthUser を gin.Context に格納し、失敗すると 401 で中断する。
func (v *SupabaseVerifier) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortUnauthorized(c, "認証トークンがありません")
			return
		}
		if !v.verifyAndSetUser(c, raw) {
			return
		}
		c.Next()
	}
}

// OptionalAuth は Authorization ヘッダがある場合のみ JWT を検証する gin ミドルウェアを返す。
//
// ヘッダなし → 匿名として c.Next() を呼ぶ。
// ヘッダありでトークンが無効 → 401 で中断する。
// ヘッダありで有効 → AuthUser を格納して c.Next() を呼ぶ。
func (v *SupabaseVerifier) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			// ヘッダなし → 匿名として続行する。
			c.Next()
			return
		}
		if !v.verifyAndSetUser(c, raw) {
			return
		}
		c.Next()
	}
}

// verifyAndSetUser はトークンを検証し、成功時に AuthUser を Context に格納する。
// 検証失敗時は 401 を返して false を返す。
func (v *SupabaseVerifier) verifyAndSetUser(c *gin.Context, raw string) bool {
	token, err := jwt.Parse(
		raw,
		v.keyfunc.Keyfunc,
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		abortUnauthorized(c, "認証トークンが無効です")
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		abortUnauthorized(c, "認証トークンが無効です")
		return false
	}

	user, ok := authUserFromClaims(claims)
	if !ok {
		abortUnauthorized(c, "認証トークンに必要な情報がありません")
		return false
	}

	c.Set(authUserContextKey, user)
	return true
}

// AuthFromContext は RequireAuth / OptionalAuth が格納した AuthUser を取り出す。
func AuthFromContext(c *gin.Context) (AuthUser, bool) {
	v, ok := c.Get(authUserContextKey)
	if !ok {
		return AuthUser{}, false
	}
	user, ok := v.(AuthUser)
	return user, ok
}

// bearerToken は "Bearer <token>" 形式のヘッダ値からトークン部を取り出す。
func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// authUserFromClaims は MapClaims から AuthUser を組み立てる。sub が無ければ false。
func authUserFromClaims(claims jwt.MapClaims) (AuthUser, bool) {
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return AuthUser{}, false
	}
	user := AuthUser{
		ID:    sub,
		Email: stringClaim(claims, "email"),
	}
	// Google 由来のプロフィール情報は Supabase JWT の user_metadata に入る。
	// 初回ログイン時の初期値（表示名・アバター・自己紹介）として取り込む。
	// 2 回目以降は repository.Upsert が上書きしないため、ユーザー編集が優先される。
	if meta, ok := claims["user_metadata"].(map[string]any); ok {
		user.DisplayName = firstNonEmpty(stringClaim(meta, "full_name"), stringClaim(meta, "name"))
		user.AvatarURL = firstNonEmpty(stringClaim(meta, "avatar_url"), stringClaim(meta, "picture"))
		if user.Email == "" {
			user.Email = stringClaim(meta, "email")
		}
		user.Description = stringClaim(meta, "description")
	}
	return user, true
}

// stringClaim は map から文字列の値を安全に取り出す(無ければ空文字)。
func stringClaim(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// firstNonEmpty は最初の空でない文字列を返す。すべて空なら空文字を返す。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// abortUnauthorized は統一フォーマットで 401 を返して処理を中断する。
func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", message))
}
