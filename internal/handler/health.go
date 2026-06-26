// Package handler は HTTP ハンドラ（入出力の変換）を実装する。ロジックは持たない。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// HealthHandler はヘルスチェック系のエンドポイントを担当する。
type HealthHandler struct{}

// NewHealthHandler は HealthHandler を生成する。
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Check godoc
//
//	@Summary		ヘルスチェック
//	@Description	サーバーが正常に稼働しているか確認する
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	model.HealthResponse
//	@Router			/health [get]
func (h *HealthHandler) Check(c *gin.Context) {
	c.JSON(http.StatusOK, model.HealthResponse{Status: "ok"})
}
