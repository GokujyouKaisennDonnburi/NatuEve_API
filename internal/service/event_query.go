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
	// maxSearchKeywords は AND 検索で受け付けるキーワードの最大件数。
	// クエリ肥大化・過剰な JOIN/サブクエリ生成を防ぐため上限を設ける。超過分は切り捨てる。
	maxSearchKeywords = 10
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
// keywords が空（トリム後に有効な語が無い）の場合は全件一覧を返す。
// 有効な語がある場合は AND 検索を行う: 各キーワードは title/description/主催者名(display_name)/
// location/持ち物(event_item) を横断（OR・部分一致・大文字小文字無視）し、キーワード間は AND で結合する。
//
// 正規化ルール:
//   - keywords は各要素を前後トリムし、空要素を除去。maxSearchKeywords(10) 件を超えた分は切り捨てる
//   - limit が 0 以下 → defaultLimit(20)
//   - limit が maxLimit(100) 超過 → maxLimit(100)
//   - offset が負値 → 0
//   - sort が許可値("created_at"/"event_date")以外 → defaultSort("created_at")
//   - order が許可値("asc"/"desc")以外 → defaultOrder("desc")
func (s *EventQueryService) List(ctx context.Context, keywords []string, sort, order string, limit, offset int) (model.EventListResponse, error) {
	keywords = normalizeKeywords(keywords)
	limit = normalizeLimit(limit)
	offset = normalizeOffset(offset)
	sort = normalizeSort(sort)
	order = normalizeOrder(order)

	var (
		summaries  []model.EventSummary
		totalCount int
		err        error
	)

	if len(keywords) == 0 {
		summaries, err = s.repo.ListSummaries(ctx, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSummaries(ctx)
		if err != nil {
			return model.EventListResponse{}, err
		}
	} else {
		summaries, err = s.repo.SearchSummaries(ctx, keywords, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSearchSummaries(ctx, keywords)
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

// normalizeKeywords は各キーワードを前後トリムし、空要素を除去する。
// 有効な語が maxSearchKeywords 件に達したらそれ以降は切り捨てる。
// 有効な語が無い場合は nil を返す（呼び出し元は全件一覧に分岐する）。
func normalizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	out := make([]string, 0, len(keywords))
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out = append(out, k)
		if len(out) >= maxSearchKeywords {
			break
		}
	}
	return out
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
