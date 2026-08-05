package middleware

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ValidateQuery は不正なクエリ文字列を 400 で弾く gin ミドルウェア。
//
// url.URL.Query() はパース失敗を握りつぶして空マップを返すため、ハンドラからは
// 「そのパラメータが指定されなかった」のと区別できない。ここで ParseQuery を
// 明示的に呼びエラーを検査し、条件が黙って捨てられたまま 200 を返す状態を防ぐ。
//
// 弾くのは以下の構文レベルのエラーのみ。未知のパラメータ名や値の妥当性は
// 各ハンドラの責務。
//   - パラメータ数が 10,000 超（CVE-2025-61726 の修正で Go 1.24.12 / 1.25.6 以降）
//   - 不正なパーセントエンコード（例: %zz、切り詰められた %E3%8）
//   - エンコードされていないセミコロンを含む
func ValidateQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw := c.Request.URL.RawQuery; raw != "" {
			if _, err := url.ParseQuery(raw); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, model.NewErrorResponse(
					"invalid_query",
					"クエリ文字列が不正です",
				))
				return
			}
		}
		c.Next()
	}
}
