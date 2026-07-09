package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// stubEventParticipationLogRepository は EventParticipationLogRepository のテスト用スタブ。
type stubEventParticipationLogRepository struct {
	// GetLatest 返却値。
	latestLog model.EventParticipationLog
	latestErr error
	// 呼び出し時に GetLatest へ渡された引数を記録する。
	gotEventID   uuid.UUID
	gotProfileID uuid.UUID

	// Create 返却値（createCreatedAt は成功時に log.ID / log.CreatedAt へセットする）。
	createID        uuid.UUID
	createCreatedAt time.Time
	createErr       error
	// 呼び出し時に Create へ渡された引数を記録する。
	gotLog *model.EventParticipationLog
}

func (s *stubEventParticipationLogRepository) GetLatest(_ context.Context, eventID, profileID uuid.UUID) (model.EventParticipationLog, error) {
	s.gotEventID = eventID
	s.gotProfileID = profileID
	if s.latestErr != nil {
		return model.EventParticipationLog{}, s.latestErr
	}
	return s.latestLog, nil
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

// newParticipationLogService はテスト用に Service を組み立てる。
// eventStub は存在確認用、logStub は最新ログ取得/ログ追記用。
func newParticipationLogService(eventStub *stubEventRepository, logStub *stubEventParticipationLogRepository) *EventParticipationLogService {
	return NewEventParticipationLogService(logStub, eventStub)
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
			svc := NewEventParticipationLogService(tt.stub, &stubEventRepository{})

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

func TestEventParticipationLogService_GetLatestStatus(t *testing.T) {
	// 固定値: eventID と profileID は有効な UUID 文字列として使う。
	eventID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	profileID := "b2c3d4e5-f6a7-8901-bcde-f23456789012"
	parsedEventID := uuid.MustParse(eventID)
	parsedProfileID := uuid.MustParse(profileID)
	updatedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		// eventStub の存在確認結果。
		ownerProfileID    string
		ownerProfileIDErr error
		// logStub の GetLatest 結果。
		latestLog model.EventParticipationLog
		latestErr error
		// 入力。
		inEventID   string
		inProfileID string
		// 期待値。
		wantAction        *string
		wantParticipating bool
		wantUpdatedAtNil  bool
		wantValErr        bool
		wantNotFound      bool
		wantErr           bool
	}{
		{
			name:              "正常: 最新アクションが join → participating=true",
			ownerProfileID:    "owner-uuid",
			latestLog:         model.EventParticipationLog{Action: "join", CreatedAt: updatedAt},
			inEventID:         eventID,
			inProfileID:       profileID,
			wantAction:        strPtr("join"),
			wantParticipating: true,
		},
		{
			name:              "正常: 最新アクションが leave → participating=false",
			ownerProfileID:    "owner-uuid",
			latestLog:         model.EventParticipationLog{Action: "leave", CreatedAt: updatedAt},
			inEventID:         eventID,
			inProfileID:       profileID,
			wantAction:        strPtr("leave"),
			wantParticipating: false,
		},
		{
			name:              "正常: 履歴なし（sql.ErrNoRows）→ action=null, participating=false, updatedAt=null",
			ownerProfileID:    "owner-uuid",
			latestErr:         fmt.Errorf("latest participation log: %w", sql.ErrNoRows),
			inEventID:         eventID,
			inProfileID:       profileID,
			wantAction:        nil,
			wantParticipating: false,
			wantUpdatedAtNil:  true,
		},
		{
			name:           "異常: eventID が不正 UUID → *ValidationError",
			ownerProfileID: "owner-uuid",
			inEventID:      "not-a-uuid",
			inProfileID:    profileID,
			wantValErr:     true,
		},
		{
			name:           "異常: profileID が不正 UUID → *ValidationError",
			ownerProfileID: "owner-uuid",
			inEventID:      eventID,
			inProfileID:    "not-a-uuid",
			wantValErr:     true,
		},
		{
			name:              "異常: イベント不存在 → *NotFoundError",
			ownerProfileIDErr: repository.ErrEventNotFound,
			inEventID:         eventID,
			inProfileID:       profileID,
			wantNotFound:      true,
		},
		{
			name:           "異常: GetLatest で予期しない repo エラー → そのまま伝播",
			ownerProfileID: "owner-uuid",
			latestErr:      errors.New("db connection lost"),
			inEventID:      eventID,
			inProfileID:    profileID,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			eventStub := &stubEventRepository{
				ownerProfileID:    tt.ownerProfileID,
				ownerProfileIDErr: tt.ownerProfileIDErr,
			}
			logStub := &stubEventParticipationLogRepository{
				latestLog: tt.latestLog,
				latestErr: tt.latestErr,
			}
			svc := newParticipationLogService(eventStub, logStub)

			resp, err := svc.GetLatestStatus(context.Background(), tt.inProfileID, tt.inEventID)

			// 期待される型付きエラーの検証。
			if tt.wantValErr {
				_ = assertValidationError(t, err)
				return
			}
			if tt.wantNotFound {
				_ = assertNotFoundError(t, err)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil だった")
				}
				// ValidationError / NotFoundError は別ケースで処理済みなので、
				// ここではそれ以外のエラーであることも確認する。
				var ve *ValidationError
				var nfe *NotFoundError
				if errors.As(err, &ve) || errors.As(err, &nfe) {
					t.Fatalf("予期しない型付きエラーが返った: %T", err)
				}
				return
			}
			assertNoErr(t, err)

			// action の検証。
			if tt.wantAction == nil {
				if resp.Action != nil {
					t.Errorf("Action = %v, want nil", *resp.Action)
				}
			} else {
				if resp.Action == nil {
					t.Errorf("Action = nil, want %q", *tt.wantAction)
				} else if *resp.Action != *tt.wantAction {
					t.Errorf("Action = %q, want %q", *resp.Action, *tt.wantAction)
				}
			}

			// participating の検証。
			if resp.Participating != tt.wantParticipating {
				t.Errorf("Participating = %v, want %v", resp.Participating, tt.wantParticipating)
			}

			// updatedAt の検証。
			if tt.wantUpdatedAtNil {
				if resp.UpdatedAt != nil {
					t.Errorf("UpdatedAt = %v, want nil", *resp.UpdatedAt)
				}
			} else {
				// 履歴なし以外のケースでは updatedAt は非 nil を期待。
				if resp.UpdatedAt == nil {
					t.Errorf("UpdatedAt = nil, want non-nil")
				}
			}

			// repo への引数伝播の検証（履歴なし/異常eventID以外のケース）。
			// eventID が不正な場合は requireEventExists で弾かれるため logRepo は呼ばれない。
			// イベント不存在の場合も logRepo は呼ばれない。
			if !tt.wantValErr && !tt.wantNotFound {
				if logStub.gotEventID != parsedEventID {
					t.Errorf("logRepo gotEventID = %v, want %v", logStub.gotEventID, parsedEventID)
				}
				if logStub.gotProfileID != parsedProfileID {
					t.Errorf("logRepo gotProfileID = %v, want %v", logStub.gotProfileID, parsedProfileID)
				}
			}
		})
	}
}

// strPtr は文字列リテラルのポインタを返すテストヘルパー。
func strPtr(s string) *string {
	return &s
}
