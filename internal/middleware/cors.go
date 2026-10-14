package middleware

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// NewCORS は CORS(Cross-Origin Resource Sharing) を設定する
// Gin ミドルウェアを生成して返す。
//
// allowOrigins の各値は scheme://host[:port]（scheme は http/https）の形でなければならず、
// 1 つでも不正な値があるか、空の場合はエラーを返す（ADR-0033）。
// 各値は小文字化して設定する。照合は完全一致のみで、ワイルドカード・正規表現は受け付けない。
func NewCORS(allowOrigins []string) (gin.HandlerFunc, error) {
	if len(allowOrigins) == 0 {
		return nil, errors.New("no allowed origins")
	}
	// gin-contrib/cors はスキームを大文字小文字を区別して検証し、"HTTP://..." で panic するため小文字化して渡す。
	origins := make([]string, len(allowOrigins))
	for i, o := range allowOrigins {
		if err := validateOrigin(o); err != nil {
			return nil, err
		}
		origins[i] = strings.ToLower(o)
	}

	return cors.New(cors.Config{
		AllowOrigins: origins,
		AllowMethods: []string{
			"GET",
			"POST",
			"PUT",
			"PATCH",
			"DELETE",
			"OPTIONS",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
		},
		ExposeHeaders: []string{
			"Content-Length",
		},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}), nil
}

// validateOrigin は origin が scheme://host[:port] の形であることを確かめる（大文字小文字は区別しない）。
// gin-contrib/cors が全許可・正規表現として解釈する "*" を含む値と "/.../" の値もエラーになる（ADR-0033）。
func validateOrigin(origin string) error {
	if strings.Contains(origin, "*") {
		return fmt.Errorf("invalid origin %q: wildcard is not allowed", origin)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid origin %q: %w", origin, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid origin %q: scheme must be http or https", origin)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("invalid origin %q: host is required", origin)
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("invalid origin %q: port is empty", origin)
	}
	// パス・末尾スラッシュ・クエリ・フラグメント・ユーザー情報があれば再構成した値と一致しない。
	if !strings.EqualFold(origin, u.Scheme+"://"+u.Host) {
		return fmt.Errorf("invalid origin %q: must be scheme://host[:port] without path, query or trailing slash", origin)
	}
	return nil
}
