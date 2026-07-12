package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

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

// ForbiddenError はアクセス権限のないリソースへの操作を表す型付きエラー。
//
// handler 層が errors.As で判定し、HTTP 403 を返すために使う。
type ForbiddenError struct {
	Message string
}

// Error は error インターフェイスを実装する。
func (e *ForbiddenError) Error() string {
	return e.Message
}

// EventCommandService はイベント書き込み系のビジネスロジックを提供する。
//
// CQRS の Command 側として位置づけ、参照系（EventQueryService）とは分離する。
type EventCommandService struct {
	repo  repository.EventRepository
	store ObjectStore // nil 安全。nil の場合はキー昇格なし。
	// wake はイベントキャンセルの通知予約後にバックグラウンドワーカーを起床させる。
	// nil 安全（未設定＝呼ばない）。NotificationOutboxWorker.Wake はメソッド自体も
	// nil レシーバ安全なため、ワーカー未生成環境（Resend 未設定）でも
	// worker.Wake をそのまま渡してよい。
	wake func()
}

// NewEventCommandService は EventCommandService を生成する。
//
// store は nil でも可（未設定時はキーあり → ValidationError）。
// wake は nil でも可（未設定時は Cancel 後の起床通知を行わない）。
func NewEventCommandService(repo repository.EventRepository, store ObjectStore, wake func()) *EventCommandService {
	return &EventCommandService{repo: repo, store: store, wake: wake}
}

// defaultCancelSubject はキャンセル通知の件名省略時に補う既定件名。
const defaultCancelSubject = "【重要】イベントは主催者によりキャンセルされました"

// defaultCancelBodyPrefix はキャンセル通知の本文省略時に補う既定本文の接頭辞。
// 実際の本文は defaultCancelBodyPrefix + イベントタイトル。
const defaultCancelBodyPrefix = "対象イベント："

// Cancel はイベント主催者がイベントを取りやめ状態にし、参加者への通知メールを
// event_notification_outbox に予約する（Transactional Outbox パターン）。
//
// 通知の件名・本文は任意。省略（空文字）時はサーバーが既定文面を補う
// （件名は defaultCancelSubject、本文は "対象イベント：" + イベントタイトル）。
// 省略時でも必ず outbox への予約は行われる。
//
// 検証エラーは *ValidationError、認可エラーは *ForbiddenError として返す。
// 非冪等: 既にキャンセル済みのイベントに対する呼び出しは
// repository.ErrEventAlreadyCancelled を %w でラップして返す
// （handler 層で errors.Is により判定し 409 を返す）。
// 予約成功時は、設定されていればワーカーを起床させる（送信を早める。無くても
// ポーリングで拾われるため必須ではない）。
func (s *EventCommandService) Cancel(ctx context.Context, profileID, eventID string, req model.CancelEventRequest) (model.CancelEventResponse, error) {
	subject, body, err := validateOptionalNotificationContent(req.Subject, req.Body)
	if err != nil {
		return model.CancelEventResponse{}, err
	}

	parsedEventID, err := requireEventOwner(ctx, s.repo, profileID, eventID)
	if err != nil {
		return model.CancelEventResponse{}, err
	}

	if subject == "" {
		subject = defaultCancelSubject
	}
	if body == "" {
		title, err := s.repo.GetTitle(ctx, parsedEventID.String())
		if err != nil {
			if errors.Is(err, repository.ErrEventNotFound) {
				return model.CancelEventResponse{}, &ValidationError{Message: "指定されたイベントが存在しません"}
			}
			return model.CancelEventResponse{}, fmt.Errorf("resolve default cancel body: %w", err)
		}
		// title は投稿時検証と events.title の VARCHAR(255) で 255 文字以内に
		// 制限されるため、既定本文（接頭辞 + title）は notificationBodyMaxLen
		// (10,000) を超えない。よって補完後の本文長の再検証は不要。
		body = defaultCancelBodyPrefix + title
	}

	cancelledAt, err := s.repo.CancelWithNotification(ctx, parsedEventID, subject, body)
	if err != nil {
		if errors.Is(err, repository.ErrEventNotFound) {
			return model.CancelEventResponse{}, &ValidationError{Message: "指定されたイベントが存在しません"}
		}
		return model.CancelEventResponse{}, fmt.Errorf("cancel event: %w", err)
	}

	if s.wake != nil {
		s.wake()
	}

	return model.CancelEventResponse{
		ID:          parsedEventID.String(),
		CancelledAt: cancelledAt,
	}, nil
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
	var pm promotedMedia
	if hasKeys && s.store != nil {
		var err error
		pm, err = promoteMedia(ctx, s.store, profileID, "events",
			req.ImageObjectKeys, req.ImageFilenames, req.PdfObjectKeys, req.PdfFilenames)
		if err != nil {
			return model.CreateEventResponse{}, err
		}
	}

	// 4. repo.Create に最終キー・元ファイル名を渡す
	e := buildNewEvent(profileID, req)
	e.ImageObjectKeys = pm.imageKeys
	e.PdfObjectKeys = pm.pdfKeys
	e.ImageFilenames = pm.imageNames
	e.PdfFilenames = pm.pdfNames

	resp, err := s.repo.Create(ctx, e)
	if err != nil {
		// 5. repo.Create 失敗時: 配置済みキーを best-effort Delete（補償）
		if len(pm.allKeys) > 0 && s.store != nil {
			for _, key := range pm.allKeys {
				if delErr := s.store.Delete(ctx, key); delErr != nil {
					slog.Warn("補償削除に失敗しました", slog.String("key", key), slog.Any("error", delErr))
				}
			}
		}
		if errors.Is(err, repository.ErrTagNotFound) {
			return model.CreateEventResponse{}, &ValidationError{Message: "指定されたタグが存在しません"}
		}
		return model.CreateEventResponse{}, fmt.Errorf("create event: %w", err)
	}
	return resp, nil
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

	// Description: trim 後に必須・10,000文字以内。
	description := strings.TrimSpace(req.Description)
	if description == "" {
		return &ValidationError{Message: "説明は必須です"}
	}
	if len([]rune(description)) > 10000 {
		return &ValidationError{Message: "説明は10,000文字以内で入力してください"}
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

	// ImageFilenames / PdfFilenames（任意）: 指定時は対応するキー配列と同数。
	if len(req.ImageFilenames) > 0 && len(req.ImageFilenames) != len(req.ImageObjectKeys) {
		return &ValidationError{Message: "画像ファイル名の数が画像オブジェクトキーの数と一致しません"}
	}
	if len(req.PdfFilenames) > 0 && len(req.PdfFilenames) != len(req.PdfObjectKeys) {
		return &ValidationError{Message: "PDFファイル名の数がPDFオブジェクトキーの数と一致しません"}
	}

	// TagIDs（任意）: 各要素は空文字不可（trim 後）・有効な UUID 形式であること。
	for i, tagID := range req.TagIDs {
		v := strings.TrimSpace(tagID)
		if v == "" {
			return &ValidationError{Message: fmt.Sprintf("タグID[%d]が空です", i)}
		}
		if _, err := uuid.Parse(v); err != nil {
			return &ValidationError{Message: fmt.Sprintf("タグID[%d]の形式が不正です", i)}
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

	// TagIDs: trim した値で重複除去（順序保持）して詰め替える。
	tagIDs := dedupeTagIDs(req.TagIDs)

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
		TagIDs:          tagIDs,
	}
}

// dedupeTagIDs は tagIDs を UUID 正準形（小文字ハイフン区切り）へ正規化したうえで、
// 順序を保持しつつ重複を除去する。
//
// uuid.Parse は urn:uuid: 接頭辞・ブレース {…}・ハイフンなし32桁も受理するが、
// PostgreSQL の uuid 型は urn:uuid: 形式を拒否する（22P02）。正準形へ揃えることで、
// 検証を通過した値が DB 書き込みで 500 になる事象を防ぎ、dedupe も表記ゆれ（大文字小文字・
// 接頭辞）に依存せず安定させる（ADR-0010 の「UUID を正規化して扱う」方針と整合）。
func dedupeTagIDs(tagIDs []string) []string {
	if len(tagIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tagIDs))
	result := make([]string, 0, len(tagIDs))
	for _, tagID := range tagIDs {
		v := strings.TrimSpace(tagID)
		// validateCreateEventRequest で検証済みのため Parse は成功する前提。
		// 万一失敗しても trim 値でフォールバックする（呼び出し順の変化に対する保険）。
		if parsed, err := uuid.Parse(v); err == nil {
			v = parsed.String()
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
