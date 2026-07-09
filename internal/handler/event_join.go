package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// conflictCode は ConflictError の Code を返す。
// Code が空の場合は既定値 "conflict" を返す。
func conflictCode(ce *service.ConflictError) string {
	if ce.Code != "" {
		return ce.Code
	}
	return "conflict"
}

// Join はイベント参加申込 API。
//
//	@Summary		イベント参加
//	@Description	認証は任意。ログイン時のみ profileId が記録される。
//	@Description	Authorization ヘッダなし → 匿名参加（profileId = null）。
//	@Description	ヘッダありでトークンが無効 → 401 で中断。
//	@Description	ヘッダありで有効 → profileId を記録してログイン参加。
//	@Description	partySizeで代表者を含む参加人数を指定できる。
//	@Description	イベント定員を超える場合は409 Conflict（capacity_full）を返す。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"イベントID"
//	@Param			body	body		model.JoinEventRequest	true	"参加申込"
//	@Success		201		{object}	model.JoinEventResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		404		{object}	model.NotFoundErrorResponse
//	@Failure		409		{object}	model.JoinConflictErrorResponse	"already_joined: 既に参加しています / capacity_full: 定員に達しています"
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
//	@Failure		429		{object}	model.RateLimitedErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/join [post]
func (h *EventHandler) Join(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証情報の取得（任意）。
	// OptionalAuth ミドルウェアにより:
	//   - ヘッダなし → AuthFromContext は (_, false) を返す → 匿名参加
	//   - ヘッダありで無効 → ミドルウェアが 401 で中断済みのためここには到達しない
	//   - ヘッダありで有効 → (authUser, true)
	var profileID uuid.NullUUID
	if authUser, ok := middleware.AuthFromContext(c); ok {
		parsed, parseErr := uuid.Parse(authUser.ID)
		if parseErr != nil {
			c.JSON(
				http.StatusUnauthorized,
				model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
			)
			return
		}
		profileID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	// JSON受け取り
	var req model.JoinEventRequest
	if !bindJSON(c, &req) {
		return
	}

	// Service呼び出し
	resp, err := h.joinSvc.Join(c.Request.Context(), eventID, profileID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse("invalid_request", ve.Message),
			)
			return
		}

		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		var ce *service.ConflictError
		if errors.As(err, &ce) {
			c.JSON(
				http.StatusConflict,
				model.NewErrorResponse(conflictCode(ce), ce.Message),
			)
			return
		}

		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加申込に失敗しました"),
		)
		return
	}

	// 成功
	c.JSON(http.StatusCreated, resp)
}

// ListMembers godoc
//
//	@Summary		イベント参加者一覧取得
//	@Description	イベント主催者が、参加者一覧を取得する。主催者のみ閲覧可能。
//	@Description	profileId は匿名参加（ログインしていない）の場合 null。
//	@Description	イベント不存在は 400 invalid_request（兄弟エンドポイントと統一）。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.EventMemberListResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		403	{object}	model.ForbiddenErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/members [get]
func (h *EventHandler) ListMembers(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	eventID := c.Param("id")

	resp, err := h.joinSvc.ListMembers(c.Request.Context(), authUser.ID, eventID)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse("invalid_request", ve.Message),
			)
			return
		}

		var fe *service.ForbiddenError
		if errors.As(err, &fe) {
			c.JSON(
				http.StatusForbidden,
				model.NewErrorResponse("forbidden", fe.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		// クライアントには詳細を返さないため、調査はこのログで行う。
		slog.Error("参加者一覧取得に失敗しました",
			slog.String("event_id", eventID),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加者一覧の取得に失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}
