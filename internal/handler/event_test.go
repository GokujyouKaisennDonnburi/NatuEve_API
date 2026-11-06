package handler

import (
	"context"
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
