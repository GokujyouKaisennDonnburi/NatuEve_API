package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// ErrEventNotFound はイベントが見つからない場合のエラー。
var ErrEventNotFound = errors.New("event not found")

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
	urls PublicURLResolver
}

// NewEventQueryService は EventQueryService を生成する。
//
// publicBaseURL は公開バケットの配信ベースURL（未設定なら URL を付与しない）。
func NewEventQueryService(repo repository.EventRepository, publicBaseURL string) *EventQueryService {
	return &EventQueryService{
		repo: repo,
		urls: NewPublicURLResolver(publicBaseURL),
	}
}

// List は limit / offset / sort / order を正規化してからイベント一覧レスポンスを返す。
//
// q が空文字（前後空白トリム後）の場合は全件一覧を返す。
// q が非空の場合は title/description/主催者名(display_name)/location/持ち物(event_item) を
// 横断した部分一致（大文字小文字無視）検索を行う。
//
// 正規化ルール:
//   - limit が 0 以下 → defaultLimit(20)
//   - limit が maxLimit(100) 超過 → maxLimit(100)
//   - offset が負値 → 0
//   - sort が許可値("created_at"/"event_date")以外 → defaultSort("created_at")
//   - order が許可値("asc"/"desc")以外 → defaultOrder("desc")
func (s *EventQueryService) List(ctx context.Context, q, sort, order string, limit, offset int) (model.EventListResponse, error) {
	q = strings.TrimSpace(q)
	limit = normalizeLimit(limit)
	offset = normalizeOffset(offset)
	sort = normalizeSort(sort)
	order = normalizeOrder(order)

	var (
		summaries  []model.EventSummary
		totalCount int
		err        error
	)

	if q == "" {
		summaries, err = s.repo.ListSummaries(ctx, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSummaries(ctx)
		if err != nil {
			return model.EventListResponse{}, err
		}
	} else {
		summaries, err = s.repo.SearchSummaries(ctx, q, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSearchSummaries(ctx, q)
		if err != nil {
			return model.EventListResponse{}, err
		}
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

// GetByID は指定されたイベント ID の詳細情報を取得する。
func (s *EventQueryService) GetByID(ctx context.Context, id string) (*model.EventResponse, error) {
	event, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}

	// 公開バケットの完全URLを付与する（ベースURL未設定なら空配列）。
	// object_key は移行時の差し替え用途や本文インライン参照のために残す。
	event.ImageUrls = s.urls.URLs(event.ImageObjectKeys)
	event.PdfUrls = s.urls.URLs(event.PdfObjectKeys)

	return event, nil
}
