package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// ErrReportNotFound はレポートが見つからない場合のエラー。
var ErrReportNotFound = errors.New("report not found")

// ReportQueryService はレポート参照系のビジネスロジックを提供する。
//
// CQRS の Query 側として位置づけ、書き込み系（ReportCommandService）とは分離する。
type ReportQueryService struct {
	repo repository.ReportRepository
	urls PublicURLResolver
}

// NewReportQueryService は ReportQueryService を生成する。
//
// publicBaseURL は公開バケットの配信ベースURL（未設定なら URL を付与しない）。
func NewReportQueryService(repo repository.ReportRepository, publicBaseURL string) *ReportQueryService {
	return &ReportQueryService{
		repo: repo,
		urls: NewPublicURLResolver(publicBaseURL),
	}
}

// GetByEventID は指定されたイベント ID に紐づくレポート詳細を取得する。
//
// 1 イベント 1 レポート（reports.event_id は UNIQUE）のため、event_id 起点で取得する。
// イベントまたはレポートが存在しない場合は ErrReportNotFound、
// イベントが取りやめ済みの場合は ErrEventCancelled を返す（ADR-0055）。
func (s *ReportQueryService) GetByEventID(ctx context.Context, eventID string) (*model.ReportResponse, error) {
	report, err := s.repo.GetByEventID(ctx, eventID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReportNotFound
		}
		if errors.Is(err, repository.ErrEventCancelled) {
			return nil, ErrEventCancelled
		}
		return nil, err
	}

	// 公開バケットの完全URLを付与する（ベースURL未設定なら空配列）。
	// object_key は移行時の差し替え用途や本文インライン参照のために残す。
	report.ImageUrls = s.urls.URLs(report.ImageObjectKeys)
	report.PdfUrls = s.urls.URLs(report.PdfObjectKeys)

	return report, nil
}
