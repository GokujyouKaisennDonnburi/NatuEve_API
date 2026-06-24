package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/service"
)

// UserHandler はユーザー(プロフィール)系のエンドポイントを担当する。
type UserHandler struct {
	svc *service.ProfileService
}

// NewUserHandler は UserHandler を生成する。
func NewUserHandler(svc *service.ProfileService) *UserHandler {
	return &UserHandler{svc: svc}
}

// GetMe godoc
//
//	@Summary		本人プロフィール取得
//	@Description	認証済みユーザー自身のプロフィールを返す(初回アクセス時に自動作成)
//	@Tags			user
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	model.ProfileResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/me [get]
func (h *UserHandler) GetMe(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	profile, err := h.svc.GetOrCreate(c.Request.Context(), service.AuthenticatedUser{
		ID:          authUser.ID,
		Email:       authUser.Email,
		DisplayName: authUser.DisplayName,
		AvatarURL:   authUser.AvatarURL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "プロフィールの取得に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, model.NewProfileResponse(profile))
}
