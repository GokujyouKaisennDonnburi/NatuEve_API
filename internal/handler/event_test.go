package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// stubSitemapEventRepo は ListSitemapEvents だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubSitemapEventRepo struct {
	repository.EventRepository
	events []model.SitemapEvent
	err    error
}

func (s *stubSitemapEventRepo) ListSitemapEvents(context.Context, int) ([]model.SitemapEvent, error) {
	return s.events, s.err
}

func TestEventHandler_ListSitemapEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name             string
		repo             *stubSitemapEventRepo
		wantStatus       int
		wantBody         string
		wantCacheControl string
	}{
		{
			name: "正常: id と updatedAt だけを返し Cache-Control を付ける",
			repo: &stubSitemapEventRepo{events: []model.SitemapEvent{
				{ID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890", UpdatedAt: updatedAt},
			}},
			wantStatus:       http.StatusOK,
			wantBody:         `[{"id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","updatedAt":"2026-01-02T03:04:05Z"}]`,
			wantCacheControl: "public, max-age=3600",
		},
		{
			name:             "正常: 0件は空配列を返す",
			repo:             &stubSitemapEventRepo{events: []model.SitemapEvent{}},
			wantStatus:       http.StatusOK,
			wantBody:         `[]`,
			wantCacheControl: "public, max-age=3600",
		},
		{
			name:       "異常: 取得失敗は 500 で Cache-Control を付けない",
			repo:       &stubSitemapEventRepo{err: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"sitemap用イベント一覧の取得に失敗しました"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(service.NewEventQueryService(tt.repo, ""), nil, nil, nil)
			r := gin.New()
			r.GET("/api/v1/sitemap/events", h.ListSitemapEvents)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/sitemap/events", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Cache-Control"); got != tt.wantCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCacheControl)
			}

			var got, want any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse response body %q: %v", w.Body.String(), err)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &want); err != nil {
				t.Fatalf("failed to parse wantBody %q: %v", tt.wantBody, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("body = %s, want %s", w.Body.String(), tt.wantBody)
			}
		})
	}
}

// stubGetByIDEventRepo は GetByID だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubGetByIDEventRepo struct {
	repository.EventRepository
	event *model.EventResponse
	err   error
}

func (s *stubGetByIDEventRepo) GetByID(context.Context, string) (*model.EventResponse, error) {
	return s.event, s.err
}

func TestEventHandler_GetByID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const eventID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	cancelledAt := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		repo       *stubGetByIDEventRepo
		wantStatus int
		wantBody   string // 空なら本文を比較しない
	}{
		{
			name:       "正常: 取りやめていないイベントは 200",
			repo:       &stubGetByIDEventRepo{event: &model.EventResponse{ID: eventID, Title: "観察会"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "異常: 取りやめ済みは 410 でイベントの内容を返さない",
			repo:       &stubGetByIDEventRepo{event: &model.EventResponse{ID: eventID, Title: "観察会", CancelledAt: &cancelledAt}},
			wantStatus: http.StatusGone,
			wantBody:   `{"error":{"code":"event_cancelled","message":"このイベントは削除されました"}}`,
		},
		{
			name:       "異常: 存在しないイベントは 404",
			repo:       &stubGetByIDEventRepo{err: sql.ErrNoRows},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"イベントが見つかりません"}}`,
		},
		{
			name:       "異常: 取得失敗は 500",
			repo:       &stubGetByIDEventRepo{err: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"イベントの取得に失敗しました"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(service.NewEventQueryService(tt.repo, ""), nil, nil, nil)
			r := gin.New()
			r.GET("/api/v1/events/:id", h.GetByID)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+eventID, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantBody != "" {
				assertJSONEqual(t, w.Body.Bytes(), tt.wantBody)
			}
		})
	}
}

// assertJSONEqual はレスポンス本文が want と JSON として一致することを確かめる。
func assertJSONEqual(t *testing.T, body []byte, want string) {
	t.Helper()
	var got, wantValue any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to parse response body %q: %v", body, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("failed to parse want %q: %v", want, err)
	}
	if !reflect.DeepEqual(got, wantValue) {
		t.Errorf("body = %s, want %s", body, want)
	}
}
