package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
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
//	@Description	主催者名/地域名/持ち物を横断して部分一致・大文字小文字無視で判定し、全語に一致するイベントを返す。未指定なら全件（最大10語）。
//	@Tags			event
//	@Produce		json
//	@Param			q		query		[]string	false	"検索キーワード(反復指定でAND検索。各語を5項目横断・部分一致・大小無視。最大10件)"	collectionFormat(multi)
//	@Param			sort	query		string	false	"ソートカラム(created_at|event_date, default: created_at)"
//	@Param			order	query		string	false	"ソート順(asc|desc, default: desc)"
//	@Param			limit	query		int		false	"取得件数(default 20, 最大 100)"
//	@Param			offset	query		int		false	"取得開始位置(default 0)"
//	@Success		200		{object}	model.EventListResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events [get]
func (h *EventHandler) List(c *gin.Context) {
	// クエリパラメータを取得する（不正値は service 層で安全側に丸める）。
	// q は反復クエリ(?q=a&q=b)で複数受け取り AND 検索する（正規化は service 層）。
	keywords := c.QueryArray("q")
	sort := c.Query("sort")
	order := c.Query("order")
	limit := queryInt(c, "limit", 0)
	offset := queryInt(c, "offset", 0)

	resp, err := h.querySvc.List(c.Request.Context(), keywords, sort, order, limit, offset)
	if err != nil {
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
//	@Description	指定されたイベントIDの詳細情報を取得する
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
