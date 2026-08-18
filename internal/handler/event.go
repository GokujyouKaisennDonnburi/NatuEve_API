package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// EventHandler はイベント系のエンドポイントを担当する。
type EventHandler struct {
	querySvc   *service.EventQueryService
	cmdSvc     *service.EventCommandService
	profileSvc *service.ProfileService
	joinSvc    *service.EventJoinService
}

// NewEventHandler は EventHandler を生成する。
func NewEventHandler(
	querySvc *service.EventQueryService,
	cmdSvc *service.EventCommandService,
	profileSvc *service.ProfileService,
	joinSvc *service.EventJoinService,
) *EventHandler {
	return &EventHandler{
		querySvc:   querySvc,
		cmdSvc:     cmdSvc,
		profileSvc: profileSvc,
		joinSvc:    joinSvc,
	}

}

// List godoc
//
//	@Summary		イベント一覧取得
//	@Description	公開イベントを指定ソート順で返す。認証不要。
//	@Description	sort は "created_at"(デフォルト) / "event_date" のみ許可。不正値はデフォルトに戻す。
//	@Description	order は "desc"(デフォルト) / "asc" のみ許可。不正値はデフォルトに戻す。
//	@Description	prifileはProfileSummaryを返す。
//	@Description	q は検索キーワード。反復指定で AND 検索になる（例: ?q=桜&q=東京）。各語はタイトル/イベント詳細/
//	@Description	主催者名/地域名/持ち物を横断して部分一致で判定し、全語に一致するイベントを返す。未指定なら全件（最大10語）。
//	@Description	照合は大文字小文字を無視し、半角/全角も同一視する（NFKC正規化。全角数字↔半角数字・全角英字↔半角英字・半角カナ↔全角カナ）。
//	@Description	tagId はタグでの絞り込み。反復指定は OR 検索になる（例: ?tagId=A&tagId=B なら A または B が付いたイベント）。
//	@Description	q と同時に指定した場合は AND（キーワード条件かつタグ条件）で絞り込む。
//	@Description	UUID 形式でない値・21件以上の指定は 400 を返す（値が空の tagId は未指定として無視する）。
//	@Tags			event
//	@Produce		json
//	@Param			q		query		[]string	false	"検索キーワード(反復指定でAND検索。各語を5項目横断・部分一致・大小無視。最大10件)"	collectionFormat(multi)
//	@Param			tagId	query		[]string	false	"絞り込むタグID(UUID。反復指定でOR検索。最大20件)"	collectionFormat(multi)
//	@Param			sort	query		string	false	"ソートカラム(created_at|event_date, default: created_at)"
//	@Param			order	query		string	false	"ソート順(asc|desc, default: desc)"
//	@Param			limit	query		int		false	"取得件数(default 20, 最大 100)"
//	@Param			offset	query		int		false	"取得開始位置(default 0)"
//	@Success		200		{object}	model.EventListResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events [get]
func (h *EventHandler) List(c *gin.Context) {
	// クエリパラメータを取得する（sort/order/limit/offset の不正値は service 層で安全側に丸める）。
	// q は反復クエリ(?q=a&q=b)で複数受け取り AND 検索する（正規化は service 層）。
	// tagId も反復クエリで複数受け取るが、こちらは OR 検索。形式不正・件数超過は 400 になる。
	keywords := c.QueryArray("q")
	tagIDs := c.QueryArray("tagId")
	sort := c.Query("sort")
	order := c.Query("order")
	limit := queryInt(c, "limit", 0)
	offset := queryInt(c, "offset", 0)

	resp, err := h.querySvc.List(c.Request.Context(), keywords, tagIDs, sort, order, limit, offset)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "イベント一覧の取得に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Create godoc
//
//	@Summary		イベント投稿
//	@Description	認証済みユーザーが新規イベントを投稿する。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		model.CreateEventRequest	true	"イベント投稿リクエスト"
//	@Success		201		{object}	model.CreateEventResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events [post]
func (h *EventHandler) Create(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	// プロフィールの存在を保証する（events.profile_id FK 対応）。
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

	var req model.CreateEventRequest
	if !bindJSON(c, &req) {
		return
	}

	resp, err := h.cmdSvc.Create(c.Request.Context(), authUser.ID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "イベントの作成に失敗しました"))
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// Cancel godoc
//
//	@Summary		イベントの取りやめ（キャンセル）
//	@Description	イベント主催者がイベントを開催取りやめにする。主催者のみ実行可能。
//	@Description	非冪等: 参加者へ送る通知メールの件名・本文は任意で受け取り、省略時は
//	@Description	サーバーが既定文面を補ったうえで、キャンセル確定と同一トランザクションで
//	@Description	通知を outbox に予約する（バックグラウンドワーカーが個別送信する）。
//	@Description	既にキャンセル済みのイベントに対する呼び出しは 409 を返す。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"イベントID"
//	@Param			body	body		model.CancelEventRequest	true	"キャンセル通知リクエスト"
//	@Success		200		{object}	model.CancelEventResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		403		{object}	model.ForbiddenErrorResponse
//	@Failure		409		{object}	model.ConflictErrorResponse	"already_cancelled: このイベントは既にキャンセルされています"
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/cancel [post]
func (h *EventHandler) Cancel(c *gin.Context) {
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

	var req model.CancelEventRequest
	if !bindJSON(c, &req) {
		return
	}

	resp, err := h.cmdSvc.Cancel(c.Request.Context(), authUser.ID, eventID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}

		var fe *service.ForbiddenError
		if errors.As(err, &fe) {
			c.JSON(http.StatusForbidden, model.NewErrorResponse("forbidden", fe.Message))
			return
		}

		if errors.Is(err, repository.ErrEventAlreadyCancelled) {
			c.JSON(http.StatusConflict, model.NewErrorResponse("already_cancelled", "このイベントは既にキャンセルされています"))
			return
		}

		slog.Error("イベントのキャンセルに失敗しました",
			slog.String("event_id", eventID),
			slog.Any("error", err),
		)
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "イベントのキャンセルに失敗しました"))
		return
	}

	c.JSON(http.StatusOK, resp)
}

// queryInt はクエリパラメータを int に変換する。変換できない場合は defaultVal を返す。
func queryInt(c *gin.Context, key string, defaultVal int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetByID godoc
//
//	@Summary		イベント詳細取得
//	@Description	指定されたイベントIDの詳細情報を取得する。cancelledAt が null 以外の場合は開催取りやめ。
//	@Description	participantCount は現在申込中の参加人数の合計で、定員未設定（capacity=0）でも返す。
//	@Description	定員がある場合の残り人数は capacity - participantCount で算出する。
//	@Tags			event
//	@Produce		json
//	@Param			id	path	string	true	"イベントID"
//	@Success		200	{object}	model.EventResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id} [get]
func (h *EventHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"invalid_request",
			"id is required",
		))
		return
	}

	event, err := h.querySvc.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrEventNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse("not_found", "イベントが見つかりません"))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"internal_error",
			"イベントの取得に失敗しました",
		))
		return
	}

	c.JSON(http.StatusOK, event)
}

// ListMine godoc
//
//	@Summary		マイページのイベント一覧取得
//	@Description	認証済みユーザー自身のイベントを種別ごとに返す。
//	@Description	type は必須。"hosted"(主催したイベント) / "applied"(申し込み中イベント) /
//	@Description	"attended"(参加済みイベント) のいずれかで、未指定・不正値は 400 を返す。
//	@Description	hosted は自分が投稿したイベント全件（過去・キャンセル済みを含む）。
//	@Description	applied は申込済みでイベント終了日時(endDate)が未到来のもの、attended は終了日時を過ぎたもの。
//	@Description	参加をキャンセル(leave)したイベントと、ログインせずに申し込んだイベントは applied / attended に含まれない。
//	@Description	counts には3種別すべての件数を常に含める（タブのバッジ表示用）。totalCount は type に対応する件数。
//	@Description	events の各要素は GET /api/v1/events と同じ EventSummary。
//	@Description	sort は "created_at"(デフォルト) / "event_date" のみ許可。不正値はデフォルトに戻す。
//	@Description	order は "desc"(デフォルト) / "asc" のみ許可。不正値はデフォルトに戻す。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			type	query		string	true	"取得する種別(hosted|applied|attended)"
//	@Param			sort	query		string	false	"ソートカラム(created_at|event_date, default: created_at)"
//	@Param			order	query		string	false	"ソート順(asc|desc, default: desc)"
//	@Param			limit	query		int		false	"取得件数(default 20, 最大 100)"
//	@Param			offset	query		int		false	"取得開始位置(default 0)"
//	@Success		200		{object}	model.MyEventListResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/me/events [get]
func (h *EventHandler) ListMine(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	profileID, err := uuid.Parse(authUser.ID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"))
		return
	}

	kind := c.Query("type")
	sort := c.Query("sort")
	order := c.Query("order")
	limit := queryInt(c, "limit", 0)
	offset := queryInt(c, "offset", 0)

	resp, err := h.querySvc.ListByProfile(c.Request.Context(), profileID, kind, sort, order, limit, offset)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", ve.Message))
			return
		}
		slog.Error("マイページのイベント一覧の取得に失敗しました",
			slog.String("profile_id", profileID.String()),
			slog.String("type", kind),
			slog.Any("error", err),
		)
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "イベント一覧の取得に失敗しました"))
		return
	}

	c.JSON(http.StatusOK, resp)
}
