package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// ReportHandler はレポート投稿系のエンドポイントを担当する。
type ReportHandler struct {
	cmdSvc *service.ReportCommandService
}

// NewReportHandler は ReportHandler を生成する。
func NewReportHandler(cmdSvc *service.ReportCommandService) *ReportHandler {
	return &ReportHandler{
		cmdSvc: cmdSvc,
	}
}

// Create godoc
//
//	@Summary		レポート投稿
//	@Description	認証済みユーザーがイベントに対してレポートを投稿する。
//	@Tags			report
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		model.CreateReportRequest	true	"レポート投稿リクエスト"
//	@Success		201		{object}	model.CreateReportResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		403		{object}	model.ForbiddenErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/reports [post]
func (h *ReportHandler) Create(c *gin.Context) {
	// 認証済みユーザーを取得する。
	// 投稿者のプロフィール存在保証は不要: service 層で「投稿者＝イベント投稿者」を
	// 強制しており、イベント投稿者のプロフィールは events.profile_id の FK で保証済み。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	// リクエストボディをバインドする
	var req model.CreateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", "リクエストボディが不正です"))
		return
	}

	// レポートを作成する
	resp, err := h.cmdSvc.Create(c.Request.Context(), authUser.ID, req)
	if err != nil {
		var fe *service.ForbiddenError
		if errors.As(err, &fe) {
			c.JSON(http.StatusForbidden, model.NewErrorResponse("forbidden", fe.Message))
			return
		}
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "レポートの作成に失敗しました"))
		return
	}

	// レスポンスを返す
	c.JSON(http.StatusCreated, resp)
}
