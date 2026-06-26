package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// UploadHandler はアップロード系エンドポイントを担当する。
type UploadHandler struct {
	uploadSvc *service.UploadService
}

// NewUploadHandler は UploadHandler を生成する。
func NewUploadHandler(uploadSvc *service.UploadService) *UploadHandler {
	return &UploadHandler{uploadSvc: uploadSvc}
}

// PresignPut godoc
//
//	@Summary		アップロード用署名付き URL 発行
//	@Description	指定した kind/contentType に対応する R2 tmp 領域への PUT 署名付き URL を返す。
//	@Description	返却された uploadUrl に直接 PUT することでクライアントがファイルをアップロードできる。
//	@Description	取得した objectKey をイベント作成時の imageObjectKeys/pdfObjectKeys に渡すこと。
//	@Description	kind="image": image/jpeg, image/png のみ対応（WebP は MVP 非対応）。
//	@Description	kind="pdf": application/pdf のみ対応。
//	@Tags			upload
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		model.PresignRequest	true	"presign リクエスト"
//	@Success		200		{object}	model.PresignResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/uploads/presign [post]
func (h *UploadHandler) PresignPut(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	var req model.PresignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", "リクエストボディが不正です"))
		return
	}

	resp, err := h.uploadSvc.PresignPut(c.Request.Context(), authUser.ID, req.Kind, req.ContentType)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "署名付き URL の発行に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, resp)
}
