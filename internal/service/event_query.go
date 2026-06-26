package service

import (
	"context"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

const (
	// defaultLimit はページネーションのデフォルト取得件数。
	defaultLimit = 20
	// maxLimit はページネーションで許容する最大取得件数。
	maxLimit = 100
	// defaultSort はソートカラムのデフォルト値。
	defaultSort = "created_at"
	// defaultOrder はソート順のデフォルト値。
	defaultOrder = "desc"
)

// EventQueryService はイベント参照系のビジネスロジックを提供する。
//
// CQRS の Query 側として位置づけ、書き込み系とは分離する。
type EventQueryService struct {
	repo repository.EventRepository
}

// NewEventQueryService は EventQueryService を生成する。
func NewEventQueryService(repo repository.EventRepository) *EventQueryService {
	return &EventQueryService{repo: repo}
}

// List は limit / offset / sort / order を正規化してからイベント一覧レスポンスを返す。
//
// 正規化ルール:
//   - limit が 0 以下 → defaultLimit(20)
//   - limit が maxLimit(100) 超過 → maxLimit(100)
//   - offset が負値 → 0
//   - sort が許可値("created_at"/"event_date")以外 → defaultSort("created_at")
//   - order が許可値("asc"/"desc")以外 → defaultOrder("desc")
func (s *EventQueryService) List(ctx context.Context, sort, order string, limit, offset int) (model.EventListResponse, error) {
	limit = normalizeLimit(limit)
	offset = normalizeOffset(offset)
	sort = normalizeSort(sort)
	order = normalizeOrder(order)

	summaries, err := s.repo.ListSummaries(ctx, sort, order, limit, offset)
	if err != nil {
		return model.EventListResponse{}, err
	}

	totalCount, err := s.repo.CountSummaries(ctx)
	if err != nil {
		return model.EventListResponse{}, err
	}

	return model.EventListResponse{
		Events:     summaries,
		TotalCount: totalCount,
		Limit:      limit,
		Offset:     offset,
	}, nil
}

// normalizeLimit は limit を有効範囲(1〜maxLimit)に丸める。
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// normalizeOffset は offset の負値を 0 に丸める。
func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// normalizeSort は sort を許可値に限定する。不正値はデフォルト("created_at")を返す。
func normalizeSort(sort string) string {
	switch sort {
	case "created_at", "event_date":
		return sort
	default:
		return defaultSort
	}
}

// normalizeOrder は order を許可値に限定する。不正値はデフォルト("desc")を返す。
func normalizeOrder(order string) string {
	switch order {
	case "asc", "desc":
		return order
	default:
		return defaultOrder
	}
}
