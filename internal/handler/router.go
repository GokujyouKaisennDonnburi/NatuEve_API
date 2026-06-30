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
	cmdSvc     *service.ReportCommandService
	profileSvc *service.ProfileService
}

// NewReportHandler は ReportHandler を生成する。
func NewReportHandler(cmdSvc *service.ReportCommandService, profileSvc *service.ProfileService) *ReportHandler {
	return &ReportHandler{
		cmdSvc:     cmdSvc,
		profileSvc: profileSvc,
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
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/reports [post]
func (h *ReportHandler) Create(c *gin.Context) {
	// 認証済みユーザーを取得する
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	// プロフィールの存在を保証する（reports.profile_id FK 対応）。
	_, err := h.profileSvc.GetOrCreate(c.Request.Context(), service.AuthenticatedUser{
		ID:          authUser.ID,
		Email:       authUser.Email,
		DisplayName: authUser.DisplayName,
		AvatarURL:   authUser.AvatarURL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "プロフィールの取得に失敗しました"))
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
