package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// bindJSON はリクエストボディを dst へバインドし、エラー時はレスポンスを書いて false を返す。
//
// *http.MaxBytesError の場合は 413（request_too_large）、
// その他のエラーは 400（invalid_request）を返す。
// 成功時は true を返す。
func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			c.JSON(http.StatusRequestEntityTooLarge, model.NewErrorResponse(
				"request_too_large",
				"リクエストボディが大きすぎます（上限1MB）",
			))
			return false
		}
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"invalid_request",
			"リクエストボディが不正です",
		))
		return false
	}
	return true
}
