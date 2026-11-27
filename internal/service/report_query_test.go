package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

func TestReportQueryServiceGetByEventID(t *testing.T) {
	const (
		eventID  = "event-uuid-001"
		reportID = "report-uuid-001"
		baseURL  = "https://cdn.example.com"
	)

	// newReport は repository が返すレポートのひな形を組み立てるヘルパー。
	newReport := func() *model.ReportResponse {
		return &model.ReportResponse{
			ID:              reportID,
			EventID:         eventID,
			Content:         "イベントは盛況でした。",
			ImageObjectKeys: []string{"reports/img1.jpg"},
			PdfObjectKeys:   []string{"reports/doc1.pdf"},
			ExternalUrls:    []string{"https://example.com/report1"},
		}
	}
	// newReportNoExternalURLs は ExternalUrls を持たないひな形。
	newReportNoExternalURLs := func() *model.ReportResponse {
		return &model.ReportResponse{
			ID:              reportID,
			EventID:         eventID,
			Content:         "イベントは盛況でした。",
			ImageObjectKeys: []string{"reports/img1.jpg"},
			PdfObjectKeys:   []string{"reports/doc1.pdf"},
			ExternalUrls:    []string{},
		}
	}

	tests := []struct {
		name             string
		publicBaseURL    string
		repoResult       *model.ReportResponse
		repoErr          error
		wantErr          error // errors.Is で照合する番兵エラー（nil なら正常系）
		wantImageURLs    []string
		wantPdfURLs      []string
		wantExternalURLs []string
	}{
		{
			name:             "正常: baseURL 未設定なら URL は空配列",
			publicBaseURL:    "",
			repoResult:       newReport(),
			wantImageURLs:    []string{},
			wantPdfURLs:      []string{},
			wantExternalURLs: []string{"https://example.com/report1"},
		},
		{
			name:             "正常: baseURL 設定時は公開URLを付与する",
			publicBaseURL:    baseURL,
			repoResult:       newReport(),
			wantImageURLs:    []string{baseURL + "/reports/img1.jpg"},
			wantPdfURLs:      []string{baseURL + "/reports/doc1.pdf"},
			wantExternalURLs: []string{"https://example.com/report1"},
		},
		{
			name:             "正常: ExternalUrls が空のときは空配列を返す",
			publicBaseURL:    baseURL,
			repoResult:       newReportNoExternalURLs(),
			wantImageURLs:    []string{baseURL + "/reports/img1.jpg"},
			wantPdfURLs:      []string{baseURL + "/reports/doc1.pdf"},
			wantExternalURLs: []string{},
		},
		{
			name:          "異常: レポート不在は ErrReportNotFound に変換する",
			publicBaseURL: baseURL,
			repoErr:       fmt.Errorf("get report by event id: %w", sql.ErrNoRows),
			wantErr:       ErrReportNotFound,
		},
		{
			name:          "異常: イベント取りやめ済みは ErrEventCancelled に変換する",
			publicBaseURL: baseURL,
			repoErr:       fmt.Errorf("get report by event id %s: %w", eventID, repository.ErrEventCancelled),
			wantErr:       ErrEventCancelled,
		},
		{
			name:          "異常: その他のエラーはそのまま伝播する",
			publicBaseURL: baseURL,
			repoErr:       errors.New("db error"),
			wantErr:       nil, // ErrReportNotFound ではないことだけ確認する
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubReportRepository{
				getResult: tt.repoResult,
				getErr:    tt.repoErr,
			}
			svc := NewReportQueryService(stub, tt.publicBaseURL)

			got, err := svc.GetByEventID(context.Background(), eventID)

			// 異常系
			if tt.repoErr != nil {
				if err == nil {
					t.Fatalf("エラーを期待したが nil だった")
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("エラー種別: got %v, want errors.Is(_, %v)", err, tt.wantErr)
				}
				// sql.ErrNoRows 以外のエラーが ErrReportNotFound に誤変換されないこと。
				if !errors.Is(tt.wantErr, ErrReportNotFound) && errors.Is(err, ErrReportNotFound) {
					t.Errorf("ErrReportNotFound に誤変換された: %v", err)
				}
				return
			}

			// 正常系
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			// repository に正しい event_id が渡っているか確認。
			if stub.gotGetEventID != eventID {
				t.Errorf("eventID: got %q, want %q", stub.gotGetEventID, eventID)
			}
			if !equalStrings(got.ImageUrls, tt.wantImageURLs) {
				t.Errorf("ImageUrls: got %v, want %v", got.ImageUrls, tt.wantImageURLs)
			}
			if !equalStrings(got.PdfUrls, tt.wantPdfURLs) {
				t.Errorf("PdfUrls: got %v, want %v", got.PdfUrls, tt.wantPdfURLs)
			}
			if !equalStrings(got.ExternalUrls, tt.wantExternalURLs) {
				t.Errorf("ExternalUrls: got %v, want %v", got.ExternalUrls, tt.wantExternalURLs)
			}
			// object_key は URL 付与後も保持される（移行・本文参照用）。
			if len(got.ImageObjectKeys) != 1 || len(got.PdfObjectKeys) != 1 {
				t.Errorf("object_key が失われた: images=%v pdfs=%v", got.ImageObjectKeys, got.PdfObjectKeys)
			}
		})
	}
}

// equalStrings は文字列スライスの一致を判定するテストヘルパー。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
