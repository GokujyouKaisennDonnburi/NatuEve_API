package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// EventParticipationLogHandler はイベント参加状態ログ系のエンドポイントを担当する。
type EventParticipationLogHandler struct {
	svc *service.EventParticipationLogService
}

// NewEventParticipationLogHandler は EventHandler を生成する。
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
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	eventID := c.Param("id")

	resp, err := h.svc.GetLatestStatus(c.Request.Context(), authUser.ID, eventID)
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
		slog.Error("参加状態取得に失敗しました",
			slog.String("event_id", eventID),
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
