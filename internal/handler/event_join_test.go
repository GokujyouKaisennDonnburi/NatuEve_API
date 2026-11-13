package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// stubEventJoinRepo は Join の呼び出しだけを記録するスタブ。
// 他のメソッドは埋め込んだ nil インターフェースを経由するため、呼ばれると panic する。
type stubEventJoinRepo struct {
	repository.EventJoinRepository
	joinCalls int
}

func (s *stubEventJoinRepo) Join(
	context.Context,
	*model.EventMember,
	model.JoinNotification,
) (repository.JoinOutcome, error) {
	s.joinCalls++
	return repository.JoinOutcomeConfirmed, nil
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
			repo := &stubEventJoinRepo{}
			svc := service.NewEventJoinService(repo, nil, "https://web.example.com")
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

// stubEventJoinService は eventJoinService のテスト用スタブ。
type stubEventJoinService struct {
	confirmResp model.ConfirmJoinResponse
	confirmErr  error

	joinResp     model.JoinEventResponse
	joinErr      error
	gotProfileID uuid.NullUUID

	gotConfirmEventID uuid.UUID
	gotConfirmToken   string
}

func (s *stubEventJoinService) Join(
	_ context.Context,
	_ uuid.UUID,
	profileID uuid.NullUUID,
	_ model.JoinEventRequest,
) (model.JoinEventResponse, error) {
	s.gotProfileID = profileID
	return s.joinResp, s.joinErr
}

func (s *stubEventJoinService) ConfirmJoin(
	_ context.Context,
	eventID uuid.UUID,
	token string,
) (model.ConfirmJoinResponse, error) {
	s.gotConfirmEventID = eventID
	s.gotConfirmToken = token
	return s.confirmResp, s.confirmErr
}

func (s *stubEventJoinService) Leave(context.Context, uuid.UUID, uuid.UUID) (model.LeaveEventResponse, error) {
	panic("not implemented in handler test stub")
}

func (s *stubEventJoinService) Absence(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	model.AbsenceEventRequest,
) (model.AbsenceEventResponse, error) {
	panic("not implemented in handler test stub")
}

func (s *stubEventJoinService) GetMyApplication(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (model.MyEventApplicationResponse, error) {
	panic("not implemented in handler test stub")
}

func (s *stubEventJoinService) ListMembers(context.Context, string, string) (model.EventMemberListResponse, error) {
	panic("not implemented in handler test stub")
}

func TestEventHandler_ConfirmJoin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	tests := []struct {
		name          string
		eventID       string
		body          string
		stub          *stubEventJoinService
		wantStatus    int
		wantErrorCode string
	}{
		{
			name:       "正常: 200 と確定結果を返す",
			eventID:    eventID.String(),
			body:       `{"token":"token-abc"}`,
			stub:       &stubEventJoinService{confirmResp: model.ConfirmJoinResponse{EventID: eventID}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "異常: イベントIDが不正",
			eventID:    "not-a-uuid",
			body:       `{"token":"token-abc"}`,
			stub:       &stubEventJoinService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "異常: リクエストボディが不正",
			eventID:    eventID.String(),
			body:       `{`,
			stub:       &stubEventJoinService{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "異常: 確認リンクが無効",
			eventID:    eventID.String(),
			body:       `{"token":"token-abc"}`,
			stub:       &stubEventJoinService{confirmErr: &service.ValidationError{Message: "確認リンクが無効か、有効期限が切れています"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "異常: イベントが存在しない",
			eventID:    eventID.String(),
			body:       `{"token":"token-abc"}`,
			stub:       &stubEventJoinService{confirmErr: &service.NotFoundError{Message: "イベントが見つかりません"}},
			wantStatus: http.StatusNotFound,
		},
		{
			name:          "異常: 定員超過",
			eventID:       eventID.String(),
			body:          `{"token":"token-abc"}`,
			stub:          &stubEventJoinService{confirmErr: &service.ConflictError{Code: "capacity_full", Message: "定員に達しています"}},
			wantStatus:    http.StatusConflict,
			wantErrorCode: "capacity_full",
		},
		{
			name:       "異常: 想定外のエラー",
			eventID:    eventID.String(),
			body:       `{"token":"token-abc"}`,
			stub:       &stubEventJoinService{confirmErr: errors.New("db error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(nil, nil, nil, tt.stub)
			r := gin.New()
			r.POST("/api/v1/events/:id/join/confirm", h.ConfirmJoin)

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/events/"+tt.eventID+"/join/confirm",
				strings.NewReader(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantErrorCode != "" {
				var resp model.ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse error response: %v", err)
				}
				if resp.Error.Code != tt.wantErrorCode {
					t.Errorf("error code = %q, want %q", resp.Error.Code, tt.wantErrorCode)
				}
			}

			if tt.wantStatus == http.StatusOK {
				var resp model.ConfirmJoinResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}
				if resp.EventID != eventID {
					t.Errorf("EventID = %v, want %v", resp.EventID, eventID)
				}
				if tt.stub.gotConfirmEventID != eventID {
					t.Errorf("service に渡した EventID = %v, want %v", tt.stub.gotConfirmEventID, eventID)
				}
				if tt.stub.gotConfirmToken != "token-abc" {
					t.Errorf("service に渡した token = %q, want %q", tt.stub.gotConfirmToken, "token-abc")
				}
			}
		})
	}
}
