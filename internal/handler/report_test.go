package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// stubGetReportRepo は GetByEventID だけを実装するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubGetReportRepo struct {
	repository.ReportRepository
	report *model.ReportResponse
	err    error
}

func (s *stubGetReportRepo) GetByEventID(context.Context, string) (*model.ReportResponse, error) {
	return s.report, s.err
}

func TestReportHandler_GetByEventID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const eventID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	tests := []struct {
		name       string
		repo       *stubGetReportRepo
		wantStatus int
		wantBody   string // 空なら本文を比較しない
	}{
		{
			name:       "正常: レポートがあれば 200",
			repo:       &stubGetReportRepo{report: &model.ReportResponse{ID: "r1", EventID: eventID}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "異常: イベント取りやめ済みは 410",
			repo:       &stubGetReportRepo{err: fmt.Errorf("get report by event id: %w", repository.ErrEventCancelled)},
			wantStatus: http.StatusGone,
			wantBody:   `{"error":{"code":"event_cancelled","message":"このイベントは削除されました"}}`,
		},
		{
			name:       "異常: レポートが無ければ 404",
			repo:       &stubGetReportRepo{err: fmt.Errorf("get report by event id: %w", sql.ErrNoRows)},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"レポートが見つかりません"}}`,
		},
		{
			name:       "異常: 取得失敗は 500",
			repo:       &stubGetReportRepo{err: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal_error","message":"レポートの取得に失敗しました"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewReportHandler(nil, service.NewReportQueryService(tt.repo, ""))
			r := gin.New()
			r.GET("/api/v1/events/:id/report", h.GetByEventID)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+eventID+"/report", nil)
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
