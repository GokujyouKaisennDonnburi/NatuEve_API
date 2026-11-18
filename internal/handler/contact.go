package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// ContactHandler は公開お問い合わせ受付のHTTP入出力を担当する。
type ContactHandler struct {
	svc *service.ContactService
}

// NewContactHandler はContactHandlerを生成する。
func NewContactHandler(svc *service.ContactService) *ContactHandler {
	return &ContactHandler{svc: svc}
}

// Submit godoc
//
//	@Summary		運営へのお問い合わせを受け付ける
//	@Description	認証不要。categoryは必須、messageは任意（trim後2000文字以内）。
//	@Description	202はoutboxへの受付完了であり、メール配信完了ではない。IPごとに平均5回/分・burst 5。
//	@Tags			contact
//	@Accept			json
//	@Produce		json
//	@Param			body	body		model.ContactRequest	true	"お問い合わせ内容"
//	@Success		202		{object}	model.ContactResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
//	@Failure		429		{object}	model.RateLimitedErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/contact [post]
func (h *ContactHandler) Submit(c *gin.Context) {
	var req model.ContactRequest
	if !bindJSON(c, &req) {
		return
	}
	resp, err := h.svc.Submit(c.Request.Context(), req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse(validationCode(ve), ve.Message))
			return
		}
		slog.Error("お問い合わせの受付保存に失敗しました", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "お問い合わせを受け付けられませんでした"))
		return
	}
	c.JSON(http.StatusAccepted, resp)
}
