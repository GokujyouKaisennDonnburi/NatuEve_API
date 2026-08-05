package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

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
	// maxFilterTagIDs はタグ絞り込みで受け付けるタグIDの最大件数。
	// キーワードと異なり超過分の切り捨ては行わず検証エラーとする（後述 normalizeTagIDs）。
	maxFilterTagIDs = 20
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
// keywords・tagIDs がともに空（正規化後に条件が無い）の場合は全件一覧を返す。
// いずれかに条件がある場合は絞り込み検索を行う:
//   - キーワード: 各語は title/description/主催者名(display_name)/location/持ち物(event_item) を
//     横断（OR・部分一致・大文字小文字無視）し、語どうしは AND で結合する
//   - タグ: 複数指定時は OR（いずれかのタグを持つイベントが該当）
//   - キーワード条件とタグ条件は AND で結合する
//
// 正規化ルール:
//   - keywords は各要素を前後トリムし、空要素を除去。maxSearchKeywords(10) 件を超えた分は切り捨てる
//   - tagIDs は空要素を除去し UUID 正準形へ正規化・重複除去する。形式不正・maxFilterTagIDs(20)
//     件超過は *ValidationError を返す（切り捨てない）
//   - limit が 0 以下 → defaultLimit(20)
//   - limit が maxLimit(100) 超過 → maxLimit(100)
//   - offset が負値 → 0
//   - sort が許可値("created_at"/"event_date")以外 → defaultSort("created_at")
//   - order が許可値("asc"/"desc")以外 → defaultOrder("desc")
func (s *EventQueryService) List(ctx context.Context, keywords, tagIDs []string, sort, order string, limit, offset int) (model.EventListResponse, error) {
	normalizedTagIDs, err := normalizeTagIDs(tagIDs)
	if err != nil {
		return model.EventListResponse{}, err
	}

	filter := model.EventSearchFilter{
		Keywords: normalizeKeywords(keywords),
		TagIDs:   normalizedTagIDs,
	}
	limit = normalizeLimit(limit)
	offset = normalizeOffset(offset)
	sort = normalizeSort(sort)
	order = normalizeOrder(order)

	var (
		summaries  []model.EventSummary
		totalCount int
	)

	if filter.IsEmpty() {
		summaries, err = s.repo.ListSummaries(ctx, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSummaries(ctx)
		if err != nil {
			return model.EventListResponse{}, err
		}
	} else {
		summaries, err = s.repo.SearchSummaries(ctx, filter, sort, order, limit, offset)
		if err != nil {
			return model.EventListResponse{}, err
		}

		totalCount, err = s.repo.CountSearchSummaries(ctx, filter)
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

// normalizeTagIDs はタグ絞り込みの tagIDs を検証し、UUID 正準形へ正規化・重複除去して返す。
// 有効な要素が無い場合は nil を返す（呼び出し元はタグ条件なしとして扱う）。
//
// 空要素（?tagId= のように値が空）は「未指定」とみなして除去する。クエリ文字列では
// 「パラメータ自体を送らない」と「空文字で送る」の区別が付きにくく、キーワード(q)の
// normalizeKeywords も同じ扱いにしているため。
//
// 一方、UUID として解釈できない値と件数超過は *ValidationError として返す（handler 層が 400 にする）。
// 黙って条件を捨てると「絞り込んだのに全件返る」誤動作に見えるため、キーワードの切り捨てとは扱いを分ける。
// 件数判定は重複除去後の値に対して行う（同じタグの重複指定で上限に達しない）。
func normalizeTagIDs(tagIDs []string) ([]string, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}

	valid := make([]string, 0, len(tagIDs))
	for i, raw := range tagIDs {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, err := uuid.Parse(v); err != nil {
			return nil, &ValidationError{Message: fmt.Sprintf("タグID[%d]の形式が不正です", i)}
		}
		valid = append(valid, v)
	}

	// dedupeTagIDs が UUID 正準形（小文字ハイフン区切り）への正規化と重複除去を行う。
	// 正準形へそろえることで、大文字小文字やブレース付きなどの表記ゆれを同一視できる。
	deduped := dedupeTagIDs(valid)
	if len(deduped) > maxFilterTagIDs {
		return nil, &ValidationError{Message: fmt.Sprintf("タグIDは%d件以内で指定してください", maxFilterTagIDs)}
	}
	return deduped, nil
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
