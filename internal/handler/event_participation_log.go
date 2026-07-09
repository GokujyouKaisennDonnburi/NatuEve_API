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

// EventParticipationLogHandler はイベント参加状態ログ系のエンドポイントを担当する。
type EventParticipationLogHandler struct {
	svc *service.EventParticipationLogService
}

// NewEventParticipationLogHandler は EventParticipationLogHandler を生成する。
func NewEventParticipationLogHandler(svc *service.EventParticipationLogService) *EventParticipationLogHandler {
	return &EventParticipationLogHandler{svc: svc}
}

// GetLatestStatus はログイン中ユーザー自身の最新の参加状態を取得する API。
//
//	@Summary		イベント参加状態取得
//	@Description	認証ユーザー自身の、指定イベントに対する最新の参加状態を取得する。
//	@Description	申し込もうとしているユーザーが既に参加しているかを判定するために使う。
//	@Description	履歴が1件もない場合は action=null, participating=false, updatedAt=null を返す（200）。
//	@Description	主催者権限は不要。本人の参加状態のみを返す。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.ParticipationStatusResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/participation-logs [get]
func (h *EventParticipationLogHandler) GetLatestStatus(c *gin.Context) {
	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証情報の取得（必須）。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	profileID, parseErr := uuid.Parse(authUser.ID)
	if parseErr != nil {
		c.JSON(
			http.StatusUnauthorized,
			model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
		)
		return
	}

	resp, err := h.svc.GetLatestStatus(c.Request.Context(), eventID, profileID)
	if err != nil {
		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		// クライアントには詳細を返さないため、調査はこのログで行う。
		slog.Error("参加状態取得に失敗しました",
			slog.String("event_id", eventID.String()),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加状態の取得に失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Create はイベント参加状態ログ追加 API。
//
//	@Summary		イベント参加状態ログ追加
//	@Description	認証必須。ログインユーザーの参加状態(join/leave)を追記ログとして記録する。状態検証はせず追記のみ。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string								true	"イベントID"
//	@Param			body	body		model.CreateParticipationLogRequest	true	"参加状態"
//	@Success		201		{object}	model.ParticipationLogResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		404		{object}	model.NotFoundErrorResponse
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/participation-logs [post]
func (h *EventParticipationLogHandler) Create(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証情報の取得（必須）。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	profileID, parseErr := uuid.Parse(authUser.ID)
	if parseErr != nil {
		c.JSON(
			http.StatusUnauthorized,
			model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
		)
		return
	}

	// JSON受け取り
	var req model.CreateParticipationLogRequest
	if !bindJSON(c, &req) {
		return
	}

	// Service呼び出し
	resp, err := h.svc.Create(c.Request.Context(), eventID, profileID, req)
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

		// 想定外エラー（DB エラー等）は真因をログに残す。
		// クライアントには詳細を返さないため、調査はこのログで行う。
		slog.Error("参加状態ログの記録に失敗しました",
			slog.String("event_id", eventID.String()),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加状態の記録に失敗しました"),
		)
		return
	}

	// 成功
	c.JSON(http.StatusCreated, resp)
}
