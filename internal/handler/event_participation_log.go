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

// participationLogCreateEnabled は本エンドポイントの有効/無効を切り替えるフラグ。
// イベント参加/キャンセルは別APIへ移行したため現在は false（呼び出すと 410 を返す）。
// 実装は将来の再有効化に備え、以降のコードとして温存する。const ではなく var なのは、
// const true にすると無効時に本体が到達不能コードになり govet(unreachable) で lint が落ちるため。
var participationLogCreateEnabled = false

// Create はイベント参加状態ログ追加 API（現在は無効）。
//
//	@Summary		イベント参加状態ログ追加（廃止予定・現在無効）
//	@Description	イベント参加/キャンセルは別APIへ移行したため、本エンドポイントは現在無効。
//	@Description	呼び出すと 410 Gone を返す。実装はサーバ内に温存しており、将来再有効化する可能性がある。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Deprecated
//	@Param			id		path	string								true	"イベントID"
//	@Param			body	body	model.CreateParticipationLogRequest	true	"参加状態"
//	@Failure		410		{object}	model.GoneErrorResponse
//	@Router			/api/v1/events/{id}/participation-logs [post]
func (h *EventParticipationLogHandler) Create(c *gin.Context) {
	// イベント参加/キャンセルは別APIへ移行したため、本エンドポイントは現在無効。
	// 以降の実装は温存するが、フラグが false の間は 410 Gone を返して即時 return する。
	if !participationLogCreateEnabled {
		c.JSON(
			http.StatusGone,
			model.NewErrorResponse("gone", "このエンドポイントは現在無効です。イベント参加/キャンセルは別APIを利用してください"),
		)
		return
	}

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
