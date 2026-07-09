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

// ReportCommandService はレポート投稿書き込み系のビジネスロジックを提供する。
//
// CQRS の Command 側として位置づけ、参照系（EventQueryService）とは分離する。
type ReportCommandService struct {
	repo      repository.ReportRepository
	eventRepo repository.EventRepository
	store     ObjectStore // nil 安全。nil の場合はキー昇格なし。
}

// NewReportCommandService は ReportCommandService を生成する。
//
// store が nil の場合、画像・PDFのオブジェクトキー昇格は行わない。
func NewReportCommandService(repo repository.ReportRepository, eventRepo repository.EventRepository, store ObjectStore) *ReportCommandService {
	return &ReportCommandService{
		repo:      repo,
		eventRepo: eventRepo,
		store:     store,
	}
}

// Create は検証・整形・キー昇格を行ったうえでレポートを登録し、レスポンスを返す。
//
// 検証エラーは *ValidationError として返す。handler 層で errors.As により判定する。
func (s *ReportCommandService) Create(ctx context.Context, profileID string, req model.CreateReportRequest) (model.CreateReportResponse, error) {
	// 既存フィールド検証
	if err := validateCreateReportRequest(req); err != nil {
		return model.CreateReportResponse{}, err
	}

	// 認可チェック: 対象イベントの投稿者のみレポートを投稿できる。
	// キー昇格・INSERT より前に実施し、無駄なオブジェクト操作を避ける。
	// 認可ロジックは requireEventOwner ヘルパーに集約（fail-closed 保証）。
	if _, err := requireEventOwner(ctx, s.eventRepo, profileID, req.EventID); err != nil {
		return model.CreateReportResponse{}, err
	}

	hasKeys := len(req.ImageObjectKeys) > 0 || len(req.PdfObjectKeys) > 0
	// キーがある場合に store が未設定なら ValidationError
	if hasKeys && s.store == nil {
		return model.CreateReportResponse{}, &ValidationError{
			Message: "ストレージが設定されていないためファイルを添付できません",
		}
	}

	// キー昇格処理（store が nil または キーなしなら skip）
	var pm promotedMedia
	if hasKeys && s.store != nil {
		var err error
		pm, err = promoteMedia(ctx, s.store, profileID, "reports",
			req.ImageObjectKeys, req.ImageFilenames, req.PdfObjectKeys, req.PdfFilenames)
		if err != nil {
			return model.CreateReportResponse{}, err
		}
	}

	// repo.Create に最終キー・元ファイル名を渡す
	report := buildNewReport(req, pm)
	resp, err := s.repo.Create(ctx, &report)
	if err != nil {
		// repo.Create 失敗時: 配置済みキーを best-effort Delete（補償）
		if len(pm.allKeys) > 0 && s.store != nil {
			for _, key := range pm.allKeys {
				if delErr := s.store.Delete(ctx, key); delErr != nil {
					slog.Warn("補償削除に失敗しました", slog.String("key", key), slog.Any("error", delErr))
				}
			}
		}
		return model.CreateReportResponse{}, fmt.Errorf("create report: %w", err)
	}

	return resp, nil
}

// validateCreateReportRequest はリクエストの各フィールドを検証する。
// 問題があれば *ValidationError を返す。
func validateCreateReportRequest(req model.CreateReportRequest) error {
	// EventID: trim 後に必須・255文字以内。
	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return &ValidationError{Message: "イベントIDは必須です"}
	}
	if len([]rune(eventID)) > 255 {
		return &ValidationError{Message: "イベントIDは255文字以内で入力してください"}
	}

	// Content: trim 後に必須・10,000文字以内。
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return &ValidationError{Message: "レポート内容は必須です"}
	}
	if len([]rune(content)) > 10000 {
		return &ValidationError{Message: "レポート内容は10,000文字以内で入力してください"}
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

	// ExternalUrls（任意）: 各要素は空文字不可（trim 後）・http/https 形式・2048文字以内。
	for i, u := range req.ExternalUrls {
		v := strings.TrimSpace(u)
		if v == "" {
			return &ValidationError{Message: fmt.Sprintf("外部URL[%d]が空です", i)}
		}
		if len([]rune(v)) > 2048 {
			return &ValidationError{Message: fmt.Sprintf("外部URL[%d]は2048文字以内で入力してください", i)}
		}
		parsed, err := url.Parse(v)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return &ValidationError{Message: fmt.Sprintf("外部URL[%d]はhttp/https形式で入力してください", i)}
		}
	}

	return nil
}

// buildNewReport は検証済みリクエストから NewReport を組み立てる（文字列は trim 済み）。
//
// 昇格済みの最終キー・元ファイル名は promotedMedia から受け取る。
// ExternalUrls は昇格不要のため req から直接 trim して詰める。
func buildNewReport(req model.CreateReportRequest, pm promotedMedia) model.NewReport {
	externalUrls := make([]string, 0, len(req.ExternalUrls))
	for _, u := range req.ExternalUrls {
		externalUrls = append(externalUrls, strings.TrimSpace(u))
	}
	return model.NewReport{
		EventID:         strings.TrimSpace(req.EventID),
		Content:         strings.TrimSpace(req.Content),
		ImageObjectKeys: pm.imageKeys,
		PdfObjectKeys:   pm.pdfKeys,
		ImageFilenames:  pm.imageNames,
		PdfFilenames:    pm.pdfNames,
		ExternalUrls:    externalUrls,
	}
}
