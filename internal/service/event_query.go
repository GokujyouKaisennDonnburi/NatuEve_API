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
	// maxFilterLocations は地域絞り込みで受け付ける location の最大件数（重複除去後）。
	// タグと同じく超過分の切り捨ては行わず検証エラーとする（後述 normalizeLocations。ADR-0028、
	// 上限値は ADR-0030）。
	maxFilterLocations = 200
	// maxLocationLength は地域絞り込みの1要素あたりの最大文字数。
	// events.location の桁(VARCHAR(255))に合わせる（ADR-0028）。
	maxLocationLength = 255
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
// keywords・tagIDs・statuses・locations がすべて空（正規化後に条件が無い）の場合は全件一覧を返す。
// いずれかに条件がある場合は絞り込み検索を行う:
//   - キーワード: 各語は title/description/主催者名(display_name)/location/持ち物(event_item) を
//     横断（OR・部分一致・大文字小文字無視）し、語どうしは AND で結合する
//   - タグ: 複数指定時は OR（いずれかのタグを持つイベントが該当）
//   - 開催状況(status): 複数指定時は OR（いずれかの状況に該当するイベントが該当）（ADR-0027）
//   - 地域(location): 対象は location 単独（キーワードの5項目横断とは異なる）。複数指定時は
//     OR（いずれかに部分一致するイベントが該当）（ADR-0028）
//   - キーワード条件・タグ条件・開催状況条件・地域条件は互いに AND で結合する
//
// 正規化ルール:
//   - keywords は各要素を前後トリムし、空要素を除去。maxSearchKeywords(10) 件を超えた分は切り捨てる
//   - tagIDs は空要素を除去し UUID 正準形へ正規化・重複除去する。形式不正・maxFilterTagIDs(20)
//     件超過は *ValidationError を返す（切り捨てない）
//   - statuses は空要素を除去し、upcoming/ongoing/ended の完全一致以外は *ValidationError を
//     返す。重複除去後は定義順（upcoming → ongoing → ended）へ並べ替える（ADR-0027）
//   - locations は各要素を前後トリムし、空要素を除去する。1要素あたり maxLocationLength(255)
//     文字を超える、または重複除去後の件数が maxFilterLocations(200) 件を超える場合は
//     *ValidationError を返す（切り捨てない）。重複除去は NFKC 正規化＋小文字化した値で判定する
//     （ADR-0028、上限値は ADR-0030）
//   - limit が 0 以下 → defaultLimit(20)
//   - limit が maxLimit(100) 超過 → maxLimit(100)
//   - offset が負値 → 0
//   - sort が許可値("created_at"/"event_date")以外 → defaultSort("created_at")
//   - order が許可値("asc"/"desc")以外 → defaultOrder("desc")
func (s *EventQueryService) List(ctx context.Context, keywords, tagIDs, statuses, locations []string, sort, order string, limit, offset int) (model.EventListResponse, error) {
	normalizedTagIDs, err := normalizeTagIDs(tagIDs)
	if err != nil {
		return model.EventListResponse{}, err
	}

	normalizedStatuses, err := normalizeStatuses(statuses)
	if err != nil {
		return model.EventListResponse{}, err
	}

	normalizedLocations, err := normalizeLocations(locations)
	if err != nil {
		return model.EventListResponse{}, err
	}

	filter := model.EventSearchFilter{
		Keywords:  normalizeKeywords(keywords),
		TagIDs:    normalizedTagIDs,
		Statuses:  normalizedStatuses,
		Locations: normalizedLocations,
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

// statusOrder は normalizeStatuses が重複除去後に並べ替える定義順（ADR-0027）。
var statusOrder = []model.EventStatus{
	model.EventStatusUpcoming,
	model.EventStatusOngoing,
	model.EventStatusEnded,
}

// normalizeStatuses は開催状況絞り込みの statuses を検証し、重複除去のうえ
// 定義順（upcoming → ongoing → ended）へ並べ替えて返す。
// 有効な要素が無い場合は nil を返す（呼び出し元は開催状況条件なしとして扱う）。
//
// 空要素（?status= のように値が空）は「未指定」とみなして除去する。normalizeTagIDs と
// 同じ扱いにしている。
//
// 許可値（upcoming/ongoing/ended）との照合は完全一致で行う。大文字表記等の許可値以外は
// *ValidationError として返す（handler 層が 400 にする）。
// 値は3種のみのため、tagIDs のような件数上限は設けない。
func normalizeStatuses(statuses []string) ([]model.EventStatus, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	seen := make(map[model.EventStatus]struct{}, len(statuses))
	for i, raw := range statuses {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		status := model.EventStatus(v)
		if !status.IsValid() {
			return nil, &ValidationError{Message: fmt.Sprintf("開催状況(status)[%d]の値が不正です", i)}
		}
		seen[status] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, nil
	}

	out := make([]model.EventStatus, 0, len(seen))
	for _, status := range statusOrder {
		if _, ok := seen[status]; ok {
			out = append(out, status)
		}
	}
	return out, nil
}

// normalizeLocations は地域絞り込みの locations を検証し、trim・空要素除去のうえ重複除去して返す。
// 有効な要素が無い場合は nil を返す（呼び出し元は地域条件なしとして扱う）。
//
// 空要素（?location= のように値が空）は「未指定」とみなして除去する。normalizeTagIDs /
// normalizeStatuses と同じ扱いにしている。
//
// 重複除去の判定キーは repository.NormalizeSearchText（NFKC 正規化）＋小文字化した値とする
// （全角/半角表記・大文字小文字の違いを同一視する）。SQL へ渡す値は最初に現れた入力値そのもの
// とする（ADR-0028）。
//
// 1要素あたりの文字数が maxLocationLength(255) を超える値、および重複除去後の件数が
// maxFilterLocations(200) を超えた場合は *ValidationError として返す（handler 層が 400 にする）。
func normalizeLocations(locations []string) ([]string, error) {
	if len(locations) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(locations))
	out := make([]string, 0, len(locations))
	for i, raw := range locations {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if len([]rune(v)) > maxLocationLength {
			return nil, &ValidationError{
				Message: fmt.Sprintf("地域(location)[%d]は%d文字以内で指定してください", i, maxLocationLength),
			}
		}
		key := strings.ToLower(repository.NormalizeSearchText(v))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	if len(out) > maxFilterLocations {
		return nil, &ValidationError{
			Message: fmt.Sprintf("地域(location)は%d件以内で指定してください", maxFilterLocations),
		}
	}
	return out, nil
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

// ListByProfile は指定プロフィールのイベント一覧を種別ごとに返す。
//
// kind は "hosted"（主催）/ "applied"（申し込み中）/ "attended"（参加済み）のいずれか。
// 不正値は既定値へ丸めず *ValidationError を返す（handler 層が 400 にする）。
//
// profileID は handler 層でパース済みの uuid.UUID を受け取る（ADR-0010）。
// limit / offset / sort / order の正規化ルールは List と同じ。
// レスポンスには3種別すべての件数（counts）を含める。種別の定義と設計判断は ADR-0024 を参照。
func (s *EventQueryService) ListByProfile(ctx context.Context, profileID uuid.UUID, kind, sort, order string, limit, offset int) (model.MyEventListResponse, error) {
	eventKind := model.MyEventKind(kind)
	if !eventKind.IsValid() {
		return model.MyEventListResponse{}, &ValidationError{
			Message: "種別(type)は hosted / applied / attended のいずれかを指定してください",
		}
	}

	limit = normalizeLimit(limit)
	offset = normalizeOffset(offset)
	sort = normalizeSort(sort)
	order = normalizeOrder(order)

	summaries, err := s.repo.ListMySummaries(ctx, model.MyEventFilter{
		ProfileID: profileID,
		Kind:      eventKind,
	}, sort, order, limit, offset)
	if err != nil {
		return model.MyEventListResponse{}, err
	}

	counts, err := s.repo.CountMyEventKinds(ctx, profileID)
	if err != nil {
		return model.MyEventListResponse{}, err
	}

	return model.MyEventListResponse{
		Events:     summaries,
		Counts:     counts,
		TotalCount: counts.Of(eventKind),
		Limit:      limit,
		Offset:     offset,
	}, nil
}

// ListPublicByProfile は指定プロフィールのイベント一覧を、公開してよい種別に限って返す。
//
// kind は "hosted"（主催）/ "attended"（参加済み）のいずれか。"applied"（申し込み中）は
// 本人限定のため公開対象外で、指定された場合は未知の値と同じく *ValidationError を返す
// （handler 層が 400 にする）。公開範囲の判断は ADR-0025 を参照。
//
// 取得そのものは ListByProfile に委譲し、公開対象外の件数（applied）を落とした
// レスポンスへ詰め替える。IsPublic を満たす種別は IsValid も満たすため、委譲先の
// 検証で弾かれることはない。
//
// profileID は handler 層でパース済みの uuid.UUID を受け取る（ADR-0010）。
// limit / offset / sort / order の正規化ルールは List と同じ。
func (s *EventQueryService) ListPublicByProfile(ctx context.Context, profileID uuid.UUID, kind, sort, order string, limit, offset int) (model.ProfileEventListResponse, error) {
	eventKind := model.MyEventKind(kind)
	if !eventKind.IsPublic() {
		return model.ProfileEventListResponse{}, &ValidationError{
			Message: "種別(type)は hosted / attended のいずれかを指定してください",
		}
	}

	resp, err := s.ListByProfile(ctx, profileID, kind, sort, order, limit, offset)
	if err != nil {
		return model.ProfileEventListResponse{}, err
	}

	counts := model.ProfileEventCounts{
		Hosted:   resp.Counts.Hosted,
		Attended: resp.Counts.Attended,
	}

	return model.ProfileEventListResponse{
		Events:     resp.Events,
		Counts:     counts,
		TotalCount: counts.Of(eventKind),
		Limit:      resp.Limit,
		Offset:     resp.Offset,
	}, nil
}
