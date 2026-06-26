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
}

// NewEventHandler は EventHandler を生成する。
func NewEventHandler(querySvc *service.EventQueryService, cmdSvc *service.EventCommandService, profileSvc *service.ProfileService) *EventHandler {
	return &EventHandler{
		querySvc:   querySvc,
		cmdSvc:     cmdSvc,
		profileSvc: profileSvc,
	}
}

// List godoc
//
//	@Summary		イベント一覧取得
//	@Description	公開イベントを指定ソート順で返す。認証不要。
//	@Description	sort は "created_at"(デフォルト) / "event_date" のみ許可。不正値はデフォルトに戻す。
//	@Description	order は "desc"(デフォルト) / "asc" のみ許可。不正値はデフォルトに戻す。
//	@Tags			event
//	@Produce		json
//	@Param			sort	query		string	false	"ソートカラム(created_at|event_date, default: created_at)"
//	@Param			order	query		string	false	"ソート順(asc|desc, default: desc)"
//	@Param			limit	query		int		false	"取得件数(default 20, 最大 100)"
//	@Param			offset	query		int		false	"取得開始位置(default 0)"
//	@Success		200		{object}	model.EventListResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events [get]
func (h *EventHandler) List(c *gin.Context) {
	// クエリパラメータを取得する（不正値は service 層で安全側に丸める）。
	sort := c.Query("sort")
	order := c.Query("order")
	limit := queryInt(c, "limit", 0)
	offset := queryInt(c, "offset", 0)

	resp, err := h.querySvc.List(c.Request.Context(), sort, order, limit, offset)
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
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse("invalid_request", "リクエストボディが不正です"))
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
