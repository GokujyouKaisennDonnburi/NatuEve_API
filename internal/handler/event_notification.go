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

// EventNotificationHandler はイベント参加者への一斉送信エンドポイントを担当する。
type EventNotificationHandler struct {
	svc *service.EventNotificationService
}

// NewEventNotificationHandler は EventNotificationHandler を生成する。
func NewEventNotificationHandler(svc *service.EventNotificationService) *EventNotificationHandler {
	return &EventNotificationHandler{svc: svc}
}

// Send godoc
//
//	@Summary		イベント参加者への一斉送信
//	@Description	イベント主催者が、参加者全員へ運用通知メールを一斉送信する。送信できるのはイベント主催者のみ。
//	@Description	同一イベントからの送信は24時間あたり上限5回（ADR-0046）。上限超過時は429を返し、メールは1通も送信されない。
//	@Description	上限以内の送信は、これまでどおり全参加者へ送信される。
//	@Tags			notification
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string								true	"イベントID"
//	@Param			body	body		model.SendEventNotificationRequest	true	"一斉送信リクエスト"
//	@Success		200		{object}	model.SendEventNotificationResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		403		{object}	model.ForbiddenErrorResponse
//	@Failure		429		{object}	model.RateLimitedErrorResponse	"送信回数上限超過（notification_limit_exceeded）または送信基盤のレート制限（rate_limited）"
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/notifications [post]
func (h *EventNotificationHandler) Send(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	eventID := c.Param("id")
	if eventID == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", "id is required"))
		return
	}

	var req model.SendEventNotificationRequest
	if !bindJSON(c, &req) {
		return
	}

	resp, err := h.svc.SendBulk(c.Request.Context(), authUser.ID, eventID, req)
	if err != nil {
		var fe *service.ForbiddenError
		if errors.As(err, &fe) {
			c.JSON(http.StatusForbidden, model.NewErrorResponse("forbidden", fe.Message))
			return
		}
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse(validationCode(ve), ve.Message))
			return
		}
		// 送信回数上限超過（ADR-0046）。メールは送信されず、期間内の再試行を促す。
		if errors.Is(err, service.ErrNotificationSendLimitExceeded) {
			slog.Warn("一斉送信が回数上限により拒否されました",
				slog.String("event_id", eventID),
				slog.Any("error", err),
			)
			c.JSON(http.StatusTooManyRequests, model.NewErrorResponse("notification_limit_exceeded", "このイベントの送信回数が上限（24時間あたり5回）に達しました。しばらく待ってから再度お試しください"))
			return
		}
		// メール送信基盤のレート制限。クライアントには 429 を返し、時間をおいた
		// 再試行を促す（恒久的な失敗ではないため 500 にはしない）。
		if errors.Is(err, service.ErrMailRateLimited) {
			slog.Warn("一斉送信がレート制限されました",
				slog.String("event_id", eventID),
				slog.Any("error", err),
			)
			c.JSON(http.StatusTooManyRequests, model.NewErrorResponse("rate_limited", "送信が混み合っています。しばらく待ってから再度お試しください"))
			return
		}
		// 想定外エラー（Resend 送信失敗・DB エラー等）は真因をログに残す。
		// クライアントには詳細を返さないため、調査はこのログで行う。
		slog.Error("一斉送信に失敗しました",
			slog.String("event_id", eventID),
			slog.Any("error", err),
		)
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "通知の送信に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, resp)
}
