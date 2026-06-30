package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	ownerID, err := s.eventRepo.GetOwnerProfileID(ctx, strings.TrimSpace(req.EventID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CreateReportResponse{}, &ValidationError{Message: "指定されたイベントが存在しません"}
	}
	if err != nil {
		return model.CreateReportResponse{}, fmt.Errorf("get event owner: %w", err)
	}
	if ownerID != profileID {
		return model.CreateReportResponse{}, &ForbiddenError{Message: "このイベントにレポートを投稿する権限がありません"}
	}

	hasKeys := len(req.ImageObjectKeys) > 0 || len(req.PdfObjectKeys) > 0
	// キーがある場合に store が未設定なら ValidationError
	if hasKeys && s.store == nil {
		return model.CreateReportResponse{}, &ValidationError{
			Message: "ストレージが設定されていないためファイルを添付できません",
		}
	}

	// キー昇格処理（store が nil または キーなしなら skip）
	var finalImageKeys []string
	var finalPdfKeys []string
	var promotedKeys []string // 補償削除用

	if hasKeys && s.store != nil {
		var err error
		finalImageKeys, finalPdfKeys, promotedKeys, err = s.promoteObjects(ctx, profileID, req)
		if err != nil {
			return model.CreateReportResponse{}, err
		}
	}

	// repo.Create に最終キーを渡す
	report := buildNewReport(req, finalImageKeys, finalPdfKeys)
	resp, err := s.repo.Create(ctx, &report)
	if err != nil {
		// repo.Create 失敗時: 配置済みキーを best-effort Delete（補償）
		if len(promotedKeys) > 0 && s.store != nil {
			for _, key := range promotedKeys {
				if delErr := s.store.Delete(ctx, key); delErr != nil {
					slog.Warn("補償削除に失敗しました", slog.String("key", key), slog.Any("error", delErr))
				}
			}
		}
		return model.CreateReportResponse{}, fmt.Errorf("create report: %w", err)
	}

	return resp, nil
}

// promoteObjects は tmp キーを検証し reports/ 領域へ昇格させる。
// 返値: finalImageKeys, finalPdfKeys, promotedKeys（補償削除用全キー）, error
func (s *ReportCommandService) promoteObjects(
	ctx context.Context,
	profileID string,
	req model.CreateReportRequest,
) (finalImageKeys, finalPdfKeys, promotedKeys []string, err error) {
	for _, key := range req.ImageObjectKeys {
		finalKey, e := promoteOneObject(ctx, s.store, profileID, key, true, func(ct string) string { return buildFinalKey("reports", ct) })
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
		finalKey, e := promoteOneObject(ctx, s.store, profileID, key, false, func(ct string) string { return buildFinalKey("reports", ct) })
		if e != nil {
			for _, pk := range promotedKeys {
				if delErr := s.store.Delete(ctx, pk); delErr != nil {
					slog.Warn("昇格失敗時の補償削除に失敗しました", slog.String("key", pk), slog.Any("error", delErr))
				}
			}
			return nil, nil, nil, e
		}
		finalPdfKeys = append(finalPdfKeys, finalKey)
		promotedKeys = append(promotedKeys, finalKey)
	}

	return finalImageKeys, finalPdfKeys, promotedKeys, nil
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

	return nil
}

// buildNewReport は検証済みリクエストから NewReport を組み立てる（文字列は trim 済み）。
//
// ImageObjectKeys / PdfObjectKeys は呼び出し元が昇格済みキーを渡す。
func buildNewReport(req model.CreateReportRequest, finalImageKeys, finalPdfKeys []string) model.NewReport {
	return model.NewReport{
		EventID:         strings.TrimSpace(req.EventID),
		Content:         strings.TrimSpace(req.Content),
		ImageObjectKeys: finalImageKeys,
		PdfObjectKeys:   finalPdfKeys,
	}
}
