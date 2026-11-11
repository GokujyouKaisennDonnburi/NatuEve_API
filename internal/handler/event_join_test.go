package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// stubEventJoinRepoForHandler は Join の呼び出しだけを記録するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubEventJoinRepoForHandler struct {
	repository.EventJoinRepository
	joinCalls int
}

func (s *stubEventJoinRepoForHandler) Join(context.Context, *model.EventMember) error {
	s.joinCalls++
	return nil
}

func TestEventHandler_Join_ValidationErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")

	tests := []struct {
		name         string
		participants []model.ParticipantInput
		wantCode     string
	}{
		{
			name: "異常: 合計11人は party_size_exceeded",
			participants: []model.ParticipantInput{
				{Category: "大人", HeadCount: 11},
			},
			wantCode: "party_size_exceeded",
		},
		{
			name: "異常: Code 未設定の検証エラーは invalid_request",
			participants: []model.ParticipantInput{
				{Category: "大人", HeadCount: 1},
				{Category: "大人", HeadCount: 1},
			},
			wantCode: "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubEventJoinRepoForHandler{}
			svc := service.NewEventJoinService(repo, nil)
			h := NewEventHandler(nil, nil, nil, svc)
			r := gin.New()
			r.POST("/api/v1/events/:id/join", h.Join)

			body, err := json.Marshal(model.JoinEventRequest{
				Username:     "山田太郎",
				MailAddress:  "yamada@example.com",
				Participants: tt.participants,
			})
			if err != nil {
				t.Fatalf("リクエストの JSON 変換に失敗: %v", err)
			}

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/events/"+eventID.String()+"/join",
				bytes.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var resp model.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("レスポンスを解析できない %q: %v", w.Body.String(), err)
			}
			if resp.Error.Code != tt.wantCode {
				t.Errorf("error.code = %q, want %q", resp.Error.Code, tt.wantCode)
			}
			if repo.joinCalls != 0 {
				t.Errorf("repository.Join 呼び出し回数 = %d, want 0", repo.joinCalls)
			}
		})
	}
}
