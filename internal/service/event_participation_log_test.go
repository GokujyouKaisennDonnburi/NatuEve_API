package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// stubEventParticipationLogRepository は EventParticipationLogRepository のテスト用スタブ。
type stubEventParticipationLogRepository struct {
	// Create 返却値（createCreatedAt は成功時に log.ID / log.CreatedAt へセットする）。
	createID        uuid.UUID
	createCreatedAt time.Time
	createErr       error
	// 呼び出し時に Create へ渡された引数を記録する。
	gotLog *model.EventParticipationLog
}

func (s *stubEventParticipationLogRepository) Create(_ context.Context, log *model.EventParticipationLog) error {
	s.gotLog = log
	if s.createErr != nil {
		return s.createErr
	}
	log.ID = s.createID
	log.CreatedAt = s.createCreatedAt
	return nil
}

func TestEventParticipationLogServiceCreate(t *testing.T) {
	// 固定 UUID でテストの再現性を確保する。
	eventID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	profileID := uuid.MustParse("b2c3d4e5-f6a7-8901-bcde-f23456789012")
	logID := uuid.MustParse("c3d4e5f6-a7b8-9012-cdef-345678901234")
	createdAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		stub         *stubEventParticipationLogRepository
		req          model.CreateParticipationLogRequest
		wantValErr   bool
		wantNotFound bool
		wantErr      bool
		// 正常系: レスポンスの全フィールドを検証する。
		checkResp func(t *testing.T, resp model.ParticipationLogResponse)
		// 正常系: repo に渡った EventParticipationLog の内容を検証する。
		checkLog func(t *testing.T, stub *stubEventParticipationLogRepository)
	}{
		// --- 正常系: join ---
		{
			name: "正常: action=join - レスポンスの全フィールドが正しく返る",
			stub: &stubEventParticipationLogRepository{
				createID:        logID,
				createCreatedAt: createdAt,
			},
			req: model.CreateParticipationLogRequest{Action: "join"},
			checkResp: func(t *testing.T, resp model.ParticipationLogResponse) {
				t.Helper()
				if resp.ID != logID {
					t.Errorf("ID: got %v, want %v", resp.ID, logID)
				}
				if resp.EventID != eventID {
					t.Errorf("EventID: got %v, want %v", resp.EventID, eventID)
				}
				if resp.ProfileID != profileID {
					t.Errorf("ProfileID: got %v, want %v", resp.ProfileID, profileID)
				}
				if resp.Action != "join" {
					t.Errorf("Action: got %q, want %q", resp.Action, "join")
				}
				if !resp.CreatedAt.Equal(createdAt) {
					t.Errorf("CreatedAt: got %v, want %v", resp.CreatedAt, createdAt)
				}
			},
			checkLog: func(t *testing.T, stub *stubEventParticipationLogRepository) {
				t.Helper()
				l := stub.gotLog
				if l == nil {
					t.Fatal("gotLog が nil")
				}
				if l.EventID != eventID {
					t.Errorf("EventParticipationLog.EventID: got %v, want %v", l.EventID, eventID)
				}
				if l.ProfileID != profileID {
					t.Errorf("EventParticipationLog.ProfileID: got %v, want %v", l.ProfileID, profileID)
				}
				if l.Action != "join" {
					t.Errorf("EventParticipationLog.Action: got %q, want %q", l.Action, "join")
				}
			},
		},
		// --- 正常系: leave ---
		{
			name: "正常: action=leave - レスポンスの全フィールドが正しく返る",
			stub: &stubEventParticipationLogRepository{
				createID:        logID,
				createCreatedAt: createdAt,
			},
			req: model.CreateParticipationLogRequest{Action: "leave"},
			checkResp: func(t *testing.T, resp model.ParticipationLogResponse) {
				t.Helper()
				if resp.Action != "leave" {
					t.Errorf("Action: got %q, want %q", resp.Action, "leave")
				}
			},
			checkLog: func(t *testing.T, stub *stubEventParticipationLogRepository) {
				t.Helper()
				if stub.gotLog == nil {
					t.Fatal("gotLog が nil")
				}
				if stub.gotLog.Action != "leave" {
					t.Errorf("EventParticipationLog.Action: got %q, want %q", stub.gotLog.Action, "leave")
				}
			},
		},
		// --- バリデーションエラー ---
		{
			name:       "異常: action が不正な値（maybe）",
			stub:       &stubEventParticipationLogRepository{createCreatedAt: createdAt},
			req:        model.CreateParticipationLogRequest{Action: "maybe"},
			wantValErr: true,
		},
		{
			name:       "異常: action が空",
			stub:       &stubEventParticipationLogRepository{createCreatedAt: createdAt},
			req:        model.CreateParticipationLogRequest{Action: ""},
			wantValErr: true,
		},
		// --- repository の sentinel エラー変換 ---
		{
			name:         "異常: イベントが存在しない（NotFoundError）",
			stub:         &stubEventParticipationLogRepository{createErr: repository.ErrEventNotFound},
			req:          model.CreateParticipationLogRequest{Action: "join"},
			wantNotFound: true,
		},
		{
			name: "異常: イベントが存在しない - ラップ済みエラー（NotFoundError）",
			stub: &stubEventParticipationLogRepository{
				createErr: errors.Join(errors.New("event xxx"), repository.ErrEventNotFound),
			},
			req:          model.CreateParticipationLogRequest{Action: "join"},
			wantNotFound: true,
		},
		// --- リポジトリエラー伝播 ---
		{
			name:    "異常: repo.Create が想定外のエラーを返す",
			stub:    &stubEventParticipationLogRepository{createErr: errors.New("db error")},
			req:     model.CreateParticipationLogRequest{Action: "join"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEventParticipationLogService(tt.stub)

			resp, err := svc.Create(context.Background(), eventID, profileID, tt.req)

			switch {
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				return
			case tt.wantNotFound:
				_ = assertNotFoundError(t, err)
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				return
			}

			assertNoErr(t, err)

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
			if tt.checkLog != nil {
				tt.checkLog(t, tt.stub)
			}
		})
	}
}
