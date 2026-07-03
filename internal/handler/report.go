package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// ReportHandler はレポート系のエンドポイントを担当する。
type ReportHandler struct {
	cmdSvc   *service.ReportCommandService
	querySvc *service.ReportQueryService
}

// NewReportHandler は ReportHandler を生成する。
func NewReportHandler(cmdSvc *service.ReportCommandService, querySvc *service.ReportQueryService) *ReportHandler {
	return &ReportHandler{
		cmdSvc:   cmdSvc,
		querySvc: querySvc,
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
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
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
	if !bindJSON(c, &req) {
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

// GetByEventID godoc
//
//	@Summary		レポート取得
//	@Description	指定したイベントIDに紐づくレポートを取得する。1イベント1レポート。認証不要。
//	@Tags			report
//	@Produce		json
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.ReportResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/report [get]
func (h *ReportHandler) GetByEventID(c *gin.Context) {
	eventID := c.Param("id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", "id is required"))
		return
	}

	report, err := h.querySvc.GetByEventID(c.Request.Context(), eventID)
	if err != nil {
		if errors.Is(err, service.ErrReportNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse("not_found", "レポートが見つかりません"))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "レポートの取得に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, report)
}
