package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// stubReportRepository は ReportRepository のテスト用スタブ。
type stubReportRepository struct {
	createResult model.CreateReportResponse
	createErr    error
	// GetByEventID 用: 返却値と記録した引数。
	getResult     *model.ReportResponse
	getErr        error
	gotGetEventID string
}

func (s *stubReportRepository) Create(_ context.Context, _ *model.NewReport) (model.CreateReportResponse, error) {
	return s.createResult, s.createErr
}

func (s *stubReportRepository) GetByEventID(_ context.Context, eventID string) (*model.ReportResponse, error) {
	s.gotGetEventID = eventID
	return s.getResult, s.getErr
}

// assertForbiddenError はテストヘルパー: err が *ForbiddenError であることを確認する。
func assertForbiddenError(t *testing.T, err error) *ForbiddenError {
	t.Helper()
	if err == nil {
		t.Fatal("ForbiddenError を期待したが nil だった")
	}
	var fe *ForbiddenError
	if !errors.As(err, &fe) {
		t.Fatalf("*ForbiddenError を期待したが %T だった: %v", err, err)
	}
	return fe
}

// validReportRequest は正常系テスト用の最小限の有効なレポートリクエスト。
func validReportRequest(eventID string) model.CreateReportRequest {
	return model.CreateReportRequest{
		EventID: eventID,
		Content: "とても充実したイベントでした。参加して良かったです。",
	}
}

func TestReportCommandServiceCreate_AuthorizationCheck(t *testing.T) {
	const (
		ownerProfileID = "owner-profile-uuid-001"
		otherProfileID = "other-profile-uuid-002"
		validEventID   = "event-uuid-001"
	)

	dummyResp := model.CreateReportResponse{
		ReportID:  "report-uuid-001",
		CreatedAt: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name              string
		profileID         string
		req               model.CreateReportRequest
		ownerProfileID    string
		ownerProfileIDErr error
		wantValidationErr bool
		wantForbiddenErr  bool
		wantErr           bool
	}{
		{
			name:           "正常: イベント存在かつ投稿者一致",
			profileID:      ownerProfileID,
			req:            validReportRequest(validEventID),
			ownerProfileID: ownerProfileID,
		},
		{
			name:              "異常: 対象イベントが存在しない（ErrNoRows）",
			profileID:         ownerProfileID,
			req:               validReportRequest(validEventID),
			ownerProfileIDErr: fmt.Errorf("get event owner profile_id: %w", sql.ErrNoRows),
			wantValidationErr: true,
		},
		{
			name:             "異常: 投稿者がイベント投稿者と異なる",
			profileID:        otherProfileID,
			req:              validReportRequest(validEventID),
			ownerProfileID:   ownerProfileID,
			wantForbiddenErr: true,
		},
		{
			name:              "異常: eventRepo が予期しないエラーを返す",
			profileID:         ownerProfileID,
			req:               validReportRequest(validEventID),
			ownerProfileIDErr: errors.New("db connection error"),
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventRepoStub := &stubEventRepository{
				ownerProfileID:    tt.ownerProfileID,
				ownerProfileIDErr: tt.ownerProfileIDErr,
			}
			reportRepoStub := &stubReportRepository{
				createResult: dummyResp,
			}
			svc := NewReportCommandService(reportRepoStub, eventRepoStub, nil)

			_, err := svc.Create(context.Background(), tt.profileID, tt.req)

			if tt.wantValidationErr {
				_ = assertValidationError(t, err)
				return
			}
			if tt.wantForbiddenErr {
				_ = assertForbiddenError(t, err)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				return
			}
			assertNoErr(t, err)
		})
	}
}
