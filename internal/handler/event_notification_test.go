package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// stubNotificationLimitEventRepo は GetOwnerProfileID だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubNotificationLimitEventRepo struct {
	repository.EventRepository
	ownerID string
}

func (r *stubNotificationLimitEventRepo) GetOwnerProfileID(context.Context, string) (string, error) {
	return r.ownerID, nil
}

// stubNotificationLimitEventJoinRepo は ListRecipients だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubNotificationLimitEventJoinRepo struct {
	repository.EventJoinRepository
}

func (*stubNotificationLimitEventJoinRepo) ListRecipients(context.Context, uuid.UUID) ([]model.EventRecipient, error) {
	return []model.EventRecipient{{MailAddress: "participant@example.com"}}, nil
}

// stubNotificationLimitSendLogRepo は Reserve だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubNotificationLimitSendLogRepo struct {
	repository.EventNotificationSendLogRepository
}

func (*stubNotificationLimitSendLogRepo) Reserve(context.Context, uuid.UUID, time.Time, time.Duration, int) (int64, error) {
	return 0, repository.ErrNotificationSendLimitReached
}

func TestEventNotificationHandler_SendNotificationLimitExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ownerID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	eventID := "b1b2c3d4-e5f6-7890-abcd-ef1234567890"
	svc := service.NewEventNotificationService(
		&stubNotificationLimitEventRepo{ownerID: ownerID},
		&stubNotificationLimitEventJoinRepo{},
		&stubNotificationLimitSendLogRepo{},
		nil,
	)
	h := NewEventNotificationHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authUser", middleware.AuthUser{ID: ownerID})
		c.Next()
	})
	r.POST("/api/v1/events/:id/notifications", h.Send)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/events/"+eventID+"/notifications",
		strings.NewReader(`{"subject":"お知らせ","body":"イベントについてのお知らせです"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusTooManyRequests, w.Body.String())
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("レスポンスを解析できない %q: %v", w.Body.String(), err)
	}
	if resp.Error.Code != "notification_limit_exceeded" {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, "notification_limit_exceeded")
	}

	wantMessage := fmt.Sprintf(
		"このイベントの送信回数が上限（%d時間あたり%d回）に達しました。しばらく待ってから再度お試しください",
		int(service.NotificationSendWindow/time.Hour),
		service.MaxNotificationSendsPerEvent,
	)
	if resp.Error.Message != wantMessage {
		t.Errorf("error.message = %q, want %q", resp.Error.Message, wantMessage)
	}
}
