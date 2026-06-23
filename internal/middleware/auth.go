package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/model"
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

		token, err := jwt.Parse(
			raw,
			v.keyfunc.Keyfunc,
			jwt.WithValidMethods([]string{"RS256", "ES256"}),
			jwt.WithAudience(v.audience),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			abortUnauthorized(c, "認証トークンが無効です")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			abortUnauthorized(c, "認証トークンが無効です")
			return
		}

		user, ok := authUserFromClaims(claims)
		if !ok {
			abortUnauthorized(c, "認証トークンに必要な情報がありません")
			return
		}

		c.Set(authUserContextKey, user)
		c.Next()
	}
}

// AuthFromContext は RequireAuth が格納した AuthUser を取り出す。
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
	if meta, ok := claims["user_metadata"].(map[string]any); ok {
		user.DisplayName = firstNonEmpty(stringClaim(meta, "full_name"), stringClaim(meta, "name"))
		user.AvatarURL = firstNonEmpty(stringClaim(meta, "avatar_url"), stringClaim(meta, "picture"))
		if user.Email == "" {
			user.Email = stringClaim(meta, "email")
		}
	}
	return user, true
}

// stringClaim は map から文字列の値を安全に取り出す(無ければ空文字)。
func stringClaim(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// firstNonEmpty は最初の空でない文字列を返す。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
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
