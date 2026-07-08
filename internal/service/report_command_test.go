package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
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

func TestValidateCreateReportRequest_ExternalUrls(t *testing.T) {
	tests := []struct {
		name         string
		externalURLs []string
		wantErrMsg   string // 空文字なら正常系
	}{
		{
			name:         "正常: ExternalUrls なし",
			externalURLs: nil,
		},
		{
			name:         "正常: https URL",
			externalURLs: []string{"https://example.com/report"},
		},
		{
			name:         "正常: http URL",
			externalURLs: []string{"http://example.com/report"},
		},
		{
			name:         "正常: 複数の有効 URL",
			externalURLs: []string{"https://example.com/a", "https://other.org/b"},
		},
		{
			name:         "異常: 空文字",
			externalURLs: []string{""},
			wantErrMsg:   "外部URL[0]が空です",
		},
		{
			name:         "異常: trim 後に空文字",
			externalURLs: []string{"   "},
			wantErrMsg:   "外部URL[0]が空です",
		},
		{
			name:         "異常: 不正スキーム (ftp)",
			externalURLs: []string{"ftp://example.com/report"},
			wantErrMsg:   "外部URL[0]はhttp/https形式で入力してください",
		},
		{
			name:         "異常: スキームなし",
			externalURLs: []string{"example.com/report"},
			wantErrMsg:   "外部URL[0]はhttp/https形式で入力してください",
		},
		{
			name:         "異常: 2048文字超過",
			externalURLs: []string{"https://example.com/" + strings.Repeat("a", 2030)},
			wantErrMsg:   "外部URL[0]は2048文字以内で入力してください",
		},
		{
			name:         "異常: 2件目が不正",
			externalURLs: []string{"https://example.com/ok", "not-a-url"},
			wantErrMsg:   "外部URL[1]はhttp/https形式で入力してください",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := model.CreateReportRequest{
				EventID:      "event-uuid-001",
				Content:      "テスト内容です。充実したイベントでした。",
				ExternalUrls: tt.externalURLs,
			}
			err := validateCreateReportRequest(req)
			if tt.wantErrMsg == "" {
				if err != nil {
					t.Fatalf("エラーなしを期待したが got %v", err)
				}
				return
			}
			ve := assertValidationError(t, err)
			if ve.Message != tt.wantErrMsg {
				t.Errorf("エラーメッセージ: got %q, want %q", ve.Message, tt.wantErrMsg)
			}
		})
	}
}

func TestReportCommandServiceCreate_AuthorizationCheck(t *testing.T) {
	const (
		ownerProfileID = "b2c3d4e5-f6a7-8901-bcde-f23456789012"
		otherProfileID = "c3d4e5f6-a7b8-9012-cdef-345678901234"
		validEventID   = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
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
			name:              "異常: 対象イベントが存在しない（ErrEventNotFound）",
			profileID:         ownerProfileID,
			req:               validReportRequest(validEventID),
			ownerProfileIDErr: fmt.Errorf("event xxx: %w", repository.ErrEventNotFound),
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
