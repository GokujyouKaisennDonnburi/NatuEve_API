package handler

import (
	"errors"
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

type createTagRequest struct {
	Name string `json:"name" binding:"required"`
}

// Create はタグ作成API。
//
// @Summary タグ作成
// @Description 新しいタグを登録する。
// @Tags tag
// @Accept json
// @Produce json
// @Param request body createTagRequest true "タグ作成"
// @Success 201 {object} model.TagResponse
// @Failure 400 {object} model.ErrorResponse "入力エラー"
// @Failure 409 {object} model.ErrorResponse "タグ重複"
// @Failure 500 {object} model.InternalErrorResponse "サーバーエラー"
// @Router /api/v1/tags [post]
func (h *TagHandler) Create(
	c *gin.Context,
) {
	var req createTagRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse(
				"invalid_request",
				"タグ名を入力してください",
			),
		)
		return
	}

	tag, err := h.tagSvc.Create(
		c.Request.Context(),
		req.Name,
	)

	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmptyTagName):
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse(
					"invalid_request",
					"タグ名を入力してください",
				),
			)

		case errors.Is(err, service.ErrTagNameTooLong):
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse(
					"invalid_request",
					"タグ名は30文字以内で入力してください",
				),
			)

		case errors.Is(err, service.ErrTagAlreadyExists):
			c.JSON(
				http.StatusConflict,
				model.NewErrorResponse(
					"duplicate_tag",
					"同じタグが既に存在します",
				),
			)

		default:
			c.JSON(
				http.StatusInternalServerError,
				model.NewErrorResponse(
					"internal_error",
					"タグ作成に失敗しました",
				),
			)
		}

		return
	}

	c.JSON(
		http.StatusCreated,
		tag,
	)
}
