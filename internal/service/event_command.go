package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// ValidationError はリクエストパラメータの検証失敗を表す型付きエラー。
//
// handler 層が errors.As で判定し、HTTP 400 を返すために使う。
type ValidationError struct {
	Message string
}

// Error は error インターフェイスを実装する。
func (e *ValidationError) Error() string {
	return e.Message
}

// EventCommandService はイベント書き込み系のビジネスロジックを提供する。
//
// CQRS の Command 側として位置づけ、参照系（EventQueryService）とは分離する。
type EventCommandService struct {
	repo  repository.EventRepository
	store ObjectStore // nil 安全。nil の場合はキー昇格なし。
}

// NewEventCommandService は EventCommandService を生成する。
//
// store は nil でも可（未設定時はキーあり → ValidationError）。
func NewEventCommandService(repo repository.EventRepository, store ObjectStore) *EventCommandService {
	return &EventCommandService{repo: repo, store: store}
}

// Create は検証・整形・キー昇格を行ったうえでイベントを登録し、レスポンスを返す。
//
// 検証エラーは *ValidationError として返す。handler 層で errors.As により判定する。
func (s *EventCommandService) Create(ctx context.Context, profileID string, req model.CreateEventRequest) (model.CreateEventResponse, error) {
	// 1. 既存フィールド検証
	if err := validateCreateEventRequest(req); err != nil {
		return model.CreateEventResponse{}, err
	}

	hasKeys := len(req.ImageObjectKeys) > 0 || len(req.PdfObjectKeys) > 0

	// 2. キーがある場合に store が未設定なら ValidationError
	if hasKeys && s.store == nil {
		return model.CreateEventResponse{}, &ValidationError{
			Message: "ストレージが設定されていないためファイルを添付できません",
		}
	}

	// 3. キー昇格処理（store が nil または キーなしなら skip）
	var finalImageKeys []string
	var finalPDFKeys []string
	var promotedKeys []string // 補償削除用

	if hasKeys && s.store != nil {
		var err error
		finalImageKeys, finalPDFKeys, promotedKeys, err = s.promoteObjects(ctx, profileID, req)
		if err != nil {
			return model.CreateEventResponse{}, err
		}
	}

	// 4. repo.Create に最終キーを渡す
	e := buildNewEvent(profileID, req)
	e.ImageObjectKeys = finalImageKeys
	e.PdfObjectKeys = finalPDFKeys

	resp, err := s.repo.Create(ctx, e)
	if err != nil {
		// 5. repo.Create 失敗時: 配置済みキーを best-effort Delete（補償）
		if len(promotedKeys) > 0 && s.store != nil {
			for _, key := range promotedKeys {
				if delErr := s.store.Delete(ctx, key); delErr != nil {
					slog.Warn("補償削除に失敗しました", slog.String("key", key), slog.Any("error", delErr))
				}
			}
		}
		return model.CreateEventResponse{}, fmt.Errorf("create event: %w", err)
	}
	return resp, nil
}

// promoteObjects は tmp キーを検証し events/ 領域へ昇格させる。
// 返値: finalImageKeys, finalPDFKeys, promotedKeys（補償削除用全キー）, error
func (s *EventCommandService) promoteObjects(
	ctx context.Context,
	profileID string,
	req model.CreateEventRequest,
) (finalImageKeys, finalPDFKeys, promotedKeys []string, err error) {
	for _, key := range req.ImageObjectKeys {
		finalKey, e := promoteOneObject(ctx, s.store, profileID, key, true, func(ct string) string { return buildFinalKey("events", ct) })
		if e != nil {
			// 昇格失敗時: ここまでに昇格済みのキーを best-effort 削除してエラー返却
			for _, pk := range promotedKeys {
				if delErr := s.store.Delete(ctx, pk); delErr != nil {
					slog.Warn("昇格失敗時の補償削除に失敗しました", slog.String("key", pk), slog.Any("error", delErr))
				}
			}
			return nil, nil, nil, e
		}
		finalImageKeys = append(finalImageKeys, finalKey)
		promotedKeys = append(promotedKeys, finalKey)
	}

	for _, key := range req.PdfObjectKeys {
		finalKey, e := promoteOneObject(ctx, s.store, profileID, key, false, func(ct string) string { return buildFinalKey("events", ct) })
		if e != nil {
			for _, pk := range promotedKeys {
				if delErr := s.store.Delete(ctx, pk); delErr != nil {
					slog.Warn("昇格失敗時の補償削除に失敗しました", slog.String("key", pk), slog.Any("error", delErr))
				}
			}
			return nil, nil, nil, e
		}
		finalPDFKeys = append(finalPDFKeys, finalKey)
		promotedKeys = append(promotedKeys, finalKey)
	}

	return finalImageKeys, finalPDFKeys, promotedKeys, nil
}

// isSameContentTypeFamily は宣言 Content-Type と sniff した Content-Type が
// 同じファミリーかどうかを判定する。
//
// http.DetectContentType は JPEG を "image/jpeg"、PNG を "image/png" と返すが、
// より具体的なサブタイプを考慮するため、プレフィックス比較を行う。
func isSameContentTypeFamily(declared, detected string) bool {
	// 完全一致
	if declared == detected {
		return true
	}
	// application/pdf は "%PDF-" prefix で検出済み
	if declared == "application/pdf" && detected == "application/pdf" {
		return true
	}
	// image/* 系: image/jpeg <-> image/jpeg 等（基本は完全一致で十分）
	return false
}

// validateCreateEventRequest はリクエストの各フィールドを検証する。
// 問題があれば *ValidationError を返す。
func validateCreateEventRequest(req model.CreateEventRequest) error {
	// Title: 空白 trim 後に必須・255文字以内。
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return &ValidationError{Message: "タイトルは必須です"}
	}
	if len([]rune(title)) > 255 {
		return &ValidationError{Message: "タイトルは255文字以内で入力してください"}
	}

	// Description: trim 後に必須。
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return &ValidationError{Message: "説明は必須です"}
	}

	// Location: trim 後に必須・255文字以内。
	location := strings.TrimSpace(req.Location)
	if location == "" {
		return &ValidationError{Message: "開催場所は必須です"}
	}
	if len([]rune(location)) > 255 {
		return &ValidationError{Message: "開催場所は255文字以内で入力してください"}
	}

	// EventDate: ゼロ値不可。
	if req.EventDate.IsZero() {
		return &ValidationError{Message: "開催日時は必須です"}
	}

	// Costs: 1件以上必須。各 Category は trim 後必須・255文字以内、Cost は 0 以上。
	if len(req.Costs) == 0 {
		return &ValidationError{Message: "費用情報は1件以上入力してください"}
	}
	for i, c := range req.Costs {
		cat := strings.TrimSpace(c.Category)
		if cat == "" {
			return &ValidationError{Message: fmt.Sprintf("費用[%d]のカテゴリは必須です", i)}
		}
		if len([]rune(cat)) > 255 {
			return &ValidationError{Message: fmt.Sprintf("費用[%d]のカテゴリは255文字以内で入力してください", i)}
		}
		if c.Cost < 0 {
			return &ValidationError{Message: fmt.Sprintf("費用[%d]の金額は0以上で入力してください", i)}
		}
	}

	// Items（任意）: 各 Item は trim 後必須・255文字以内。
	for i, item := range req.Items {
		v := strings.TrimSpace(item.Item)
		if v == "" {
			return &ValidationError{Message: fmt.Sprintf("持ち物[%d]の名称は必須です", i)}
		}
		if len([]rune(v)) > 255 {
			return &ValidationError{Message: fmt.Sprintf("持ち物[%d]の名称は255文字以内で入力してください", i)}
		}
	}

	// Capacity（任意）: 0=未設定（許可）、負数=不正。
	if req.Capacity < 0 {
		return &ValidationError{Message: "定員は0以上で入力してください"}
	}

	// ExternalURL（任意）: 指定時は 255 文字以内かつ http/https の妥当な URL。
	externalURL := strings.TrimSpace(req.ExternalURL)
	if externalURL != "" {
		if len([]rune(externalURL)) > 255 {
			return &ValidationError{Message: "外部URLは255文字以内で入力してください"}
		}
		u, err := url.Parse(externalURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return &ValidationError{Message: "外部URLは http または https で始まる有効なURLを入力してください"}
		}
	}

	// ImageObjectKeys（任意）: 各要素は空文字不可（trim 後）。
	for i, key := range req.ImageObjectKeys {
		if strings.TrimSpace(key) == "" {
			return &ValidationError{Message: fmt.Sprintf("画像オブジェクトキー[%d]が空です", i)}
		}
	}

	// PdfObjectKeys（任意）: 各要素は空文字不可（trim 後）・255文字以内。
	for i, key := range req.PdfObjectKeys {
		v := strings.TrimSpace(key)
		if v == "" {
			return &ValidationError{Message: fmt.Sprintf("PDFオブジェクトキー[%d]が空です", i)}
		}
		if len([]rune(v)) > 255 {
			return &ValidationError{Message: fmt.Sprintf("PDFオブジェクトキー[%d]は255文字以内で入力してください", i)}
		}
	}

	return nil
}

// buildNewEvent は検証済みリクエストから NewEvent を組み立てる（文字列は trim 済み）。
func buildNewEvent(profileID string, req model.CreateEventRequest) *model.NewEvent {
	// Costs: カテゴリを trim した値で詰め替える。
	costs := make([]model.EventCostInput, len(req.Costs))
	for i, c := range req.Costs {
		costs[i] = model.EventCostInput{
			Category: strings.TrimSpace(c.Category),
			Cost:     c.Cost,
		}
	}

	// Items: Item 名を trim した値で詰め替える。
	items := make([]model.EventItemInput, len(req.Items))
	for i, item := range req.Items {
		items[i] = model.EventItemInput{
			Item:       strings.TrimSpace(item.Item),
			IsRequired: item.IsRequired,
		}
	}

	// ImageObjectKeys / PdfObjectKeys は呼び出し元が昇格済みキーをセットするため空で初期化。
	return &model.NewEvent{
		ProfileID:       profileID,
		Title:           strings.TrimSpace(req.Title),
		Description:     strings.TrimSpace(req.Description),
		Location:        strings.TrimSpace(req.Location),
		EventDate:       req.EventDate.UTC(),
		Capacity:        req.Capacity,
		ExternalURL:     strings.TrimSpace(req.ExternalURL),
		Costs:           costs,
		Items:           items,
		ImageObjectKeys: nil,
		PdfObjectKeys:   nil,
	}
}
