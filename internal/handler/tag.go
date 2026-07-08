package handler

import (
	"net/http"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
	"github.com/gin-gonic/gin"
)

// TagHandler はタグに関するHTTPハンドラを担当する。
type TagHandler struct {
	tagSvc *service.TagService
}

// NewTagHandler はHandlerを生成する。
func NewTagHandler(tagSvc *service.TagService) *TagHandler {
	return &TagHandler{
		tagSvc: tagSvc,
	}
}

// List はタグ一覧取得API
//
// @Summary タグ一覧取得
// @Description タグ一覧を取得する。
// @Tags tag
// @Produce json
// @Success 200 {object} model.TagListResponse
// @Failure 500 {object} model.InternalErrorResponse
// @Router /api/v1/tags [get]
func (h *TagHandler) List(c *gin.Context) {
	resp, err := h.tagSvc.List(c.Request.Context())
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse(
				"internal_error",
				"タグ一覧の取得に失敗しました",
			),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}
