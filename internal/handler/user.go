package handler

import (
	"errors"
	"net/http"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
	"github.com/gin-gonic/gin"
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
		Description: authUser.Description,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"internal_error",
			"プロフィールの取得に失敗しました",
		))
		return
	}

	c.JSON(http.StatusOK, model.NewProfileResponse(profile))
}

// GetProfile godoc
//
//	@Summary		ユーザープロフィール取得
//	@Description	指定したユーザー ID のプロフィールを返す
//	@Tags			user
//	@Produce		json
//	@Param			id	path	string	true	"ユーザー ID"
//	@Success		200	{object}	model.ProfilePublic
//	@Failure		404	{object}	model.NotFoundErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/profiles/{id} [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	id := c.Param("id")

	profile, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrProfileNotFound) {
			c.JSON(http.StatusNotFound,
				model.NewErrorResponse("not_found", "プロフィールが見つかりません"),
			)
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"internal_error",
			"プロフィールの取得に失敗しました",
		))
		return
	}

	c.JSON(http.StatusOK, model.NewProfilePublic(profile))
}

// UpdateMe godoc
//
//	@Summary		本人プロフィール更新
//	@Description	認証済みユーザー自身のプロフィールを更新する
//	@Tags			user
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body	model.UpdateProfileRequest	true	"更新内容"
//	@Success		200	{object}	model.ProfileResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse
//	@Failure		413	{object}	model.RequestTooLargeErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/me [patch]
func (h *UserHandler) UpdateMe(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	var req model.UpdateProfileRequest
	if !bindJSON(c, &req) {
		return
	}

	profile, err := h.svc.UpdateMyProfile(c.Request.Context(), authUser.ID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		if errors.Is(err, service.ErrProfileNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse("not_found", "プロフィールが見つかりません"))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "更新に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, model.NewProfileResponse(profile))
}
