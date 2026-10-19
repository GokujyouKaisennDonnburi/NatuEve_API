package middleware

import (
	"context"
	"net/http"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// GoogleJWKSURL は Google が OIDC トークンの署名に使う公開鍵の配布先。
const GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// googleIssuers は Google が発行する OIDC トークンの iss クレームの取りうる値。
var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// SchedulerVerifier は Cloud Scheduler が付ける OIDC トークンを検証する（ADR-0035）。
//
// 署名は Google の JWKS で検証し、aud と email（サービスアカウント）が設定値と一致する
// 呼び出しだけを通す。
type SchedulerVerifier struct {
	keyfunc  keyfunc.Keyfunc
	audience string
	email    string
}

// NewSchedulerVerifier は jwksURL（本番は GoogleJWKSURL）の JWKS から検証器を構築する。
//
// JWKS の取得・更新は keyfunc がバックグラウンドで行う。初回の取得に失敗してもエラーにはならず、
// 鍵を取得できるまでは正しいトークンも 401 になる。
func NewSchedulerVerifier(jwksURL, audience, serviceAccountEmail string) (*SchedulerVerifier, error) {
	kf, err := keyfunc.NewDefaultCtx(context.Background(), []string{jwksURL})
	if err != nil {
		return nil, err
	}
	return &SchedulerVerifier{keyfunc: kf, audience: audience, email: serviceAccountEmail}, nil
}

// RequireScheduler は Cloud Scheduler の OIDC トークンを検証する gin ミドルウェアを返す。
//
// トークンが無い・署名や有効期限、iss、aud が不正なら 401、
// 有効な Google のトークンでもサービスアカウントが違えば 403 で中断する。
func (v *SchedulerVerifier) RequireScheduler() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortUnauthorized(c, "認証トークンがありません")
			return
		}

		token, err := jwt.Parse(
			raw,
			v.keyfunc.Keyfunc,
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithAudience(v.audience),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			abortUnauthorized(c, "認証トークンが無効です")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !validIssuer(claims) {
			abortUnauthorized(c, "認証トークンが無効です")
			return
		}

		// email_verified が true（bool または文字列 "true"）で、email が設定値と完全一致する呼び出しだけを通す。
		if email := stringClaim(claims, "email"); !emailVerified(claims) || email != v.email {
			c.AbortWithStatusJSON(http.StatusForbidden, model.NewErrorResponse("forbidden", "この呼び出し元は許可されていません"))
			return
		}

		c.Next()
	}
}

// emailVerified は email_verified クレームが bool の true、または文字列の "true" なら true を返す。
func emailVerified(claims jwt.MapClaims) bool {
	switch v := claims["email_verified"].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

// validIssuer は iss クレームが Google のものであることを確認する。
func validIssuer(claims jwt.MapClaims) bool {
	iss := stringClaim(claims, "iss")
	for _, want := range googleIssuers {
		if iss == want {
			return true
		}
	}
	return false
}
