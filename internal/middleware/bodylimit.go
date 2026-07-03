package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// maxBodyBytes は全ルートに適用するリクエストボディの上限サイズ（1MB）。
const maxBodyBytes = 1 << 20

// BodyLimit は全ルートにリクエストボディの上限（1MB）を課す gin ミドルウェア。
//
// Content-Length が上限を超える場合はボディを読まず即座に 413 を返す。
// それ以外は http.MaxBytesReader でボディをラップし、チャンク転送や
// Content-Length 偽装の場合も読み取り時に強制的に上限を適用する。
func BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBodyBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, model.NewErrorResponse(
				"request_too_large",
				"リクエストボディが大きすぎます（上限1MB）",
			))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		c.Next()
	}
}
