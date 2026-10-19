package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// OutboxProcessor は outbox を 1 回分処理して、処理した件数を返す。
type OutboxProcessor interface {
	ProcessOnce(ctx context.Context) (int, error)
}

// OutboxHandler は Cloud Scheduler から呼ばれる内部エンドポイントを提供する。
// swag のアノテーションは付けない（ADR-0035）。
type OutboxHandler struct {
	processor OutboxProcessor
	// stop がキャンセルされると、処理中の ProcessOnce は新しい行の確保をやめる。
	stop context.Context
}

// NewOutboxHandler は OutboxHandler を生成する。stop にはシャットダウンのシグナルで
// キャンセルされる ctx を渡す。
func NewOutboxHandler(stop context.Context, processor OutboxProcessor) *OutboxHandler {
	return &OutboxHandler{processor: processor, stop: stop}
}

// Process は outbox を 1 回処理し、処理した件数を {"processed": n} で返す。
// ProcessOnce に渡す ctx は、リクエストの ctx と stop のどちらかがキャンセルされるとキャンセルされる。
func (h *OutboxHandler) Process(c *gin.Context) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	stopWatch := context.AfterFunc(h.stop, cancel)
	defer stopWatch()
	// AfterFunc は別 goroutine で呼ばれるため、受信時点で stop がキャンセル済みならここで止める。
	if h.stop.Err() != nil {
		cancel()
	}

	n, err := h.processor.ProcessOnce(ctx)
	if err != nil {
		slog.Error("outbox の処理に失敗しました", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse("internal_error", "outbox の処理に失敗しました"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"processed": n})
}
