package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ObjectStore は R2 などのオブジェクトストレージを抽象化するインターフェイス。
// service 層はこのインターフェイス経由でストレージを操作する。
type ObjectStore interface {
	// PresignPut は PUT 用署名付き URL を発行する。
	PresignPut(ctx context.Context, key, contentType string) (url string, expiresAt time.Time, err error)
	// Head はオブジェクトのメタデータ（サイズ・Content-Type）を返す。
	Head(ctx context.Context, key string) (size int64, contentType string, err error)
	// Get はオブジェクトの全バイトを返す。maxBytes を超えるオブジェクトはエラー。
	Get(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	// Put は body を指定キーで PUT する。
	Put(ctx context.Context, key string, body []byte, contentType string) error
	// Copy はバケット内でオブジェクトをコピーする。
	Copy(ctx context.Context, srcKey, dstKey string) error
	// Delete はオブジェクトを削除する。
	Delete(ctx context.Context, key string) error
}

// --- Allowlist 定義 ---

// allowedImageContentTypes は画像としてアップロード可能な MIME タイプ一覧。
// WebP は MVP では非対応（再エンコード先が jpeg/png の標準パッケージに限定するため）。
var allowedImageContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
}

// allowedPDFContentTypes は PDF としてアップロード可能な MIME タイプ一覧。
var allowedPDFContentTypes = map[string]bool{
	"application/pdf": true,
}

// contentTypeToExt は MIME タイプからファイル拡張子（ドット付き）へのマッピング。
var contentTypeToExt = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"application/pdf": ".pdf",
}

// --- サイズ上限定数 ---

const (
	// maxImageBytes は画像ファイルの最大サイズ（10MB）。
	maxImageBytes int64 = 10 * 1024 * 1024
	// maxPDFBytes は PDF ファイルの最大サイズ（20MB）。
	maxPDFBytes int64 = 20 * 1024 * 1024
)

// UploadService は presign URL 発行のビジネスロジックを提供する。
type UploadService struct {
	store ObjectStore
}

// NewUploadService は UploadService を生成する。
func NewUploadService(store ObjectStore) *UploadService {
	return &UploadService{store: store}
}

// PresignPut は kind/contentType を検証し、tmp 領域への署名付き PUT URL を発行する。
//
// 生成されるキー形式: natueve/tmp/{profileID}/{uuid}{ext}
func (s *UploadService) PresignPut(ctx context.Context, profileID, kind, contentType string) (model.PresignResponse, error) {
	if err := validateProfileID(profileID); err != nil {
		return model.PresignResponse{}, err
	}

	if err := validateKindContentType(kind, contentType); err != nil {
		return model.PresignResponse{}, err
	}

	ext := contentTypeToExt[contentType]
	key := fmt.Sprintf("natueve/tmp/%s/%s%s", profileID, uuid.New().String(), ext)

	uploadURL, expiresAt, err := s.store.PresignPut(ctx, key, contentType)
	if err != nil {
		return model.PresignResponse{}, fmt.Errorf("presign put: %w", err)
	}

	return model.PresignResponse{
		UploadURL: uploadURL,
		ObjectKey: key,
		ExpiresAt: expiresAt,
	}, nil
}

// validateKindContentType は kind と contentType の組み合わせを検証する。
func validateKindContentType(kind, contentType string) error {
	switch kind {
	case "image":
		if !allowedImageContentTypes[contentType] {
			return &ValidationError{
				Message: fmt.Sprintf("画像の contentType は image/jpeg または image/png のみ指定できます（指定値: %q）", contentType),
			}
		}
	case "pdf":
		if !allowedPDFContentTypes[contentType] {
			return &ValidationError{
				Message: fmt.Sprintf("PDFの contentType は application/pdf のみ指定できます（指定値: %q）", contentType),
			}
		}
	default:
		return &ValidationError{
			Message: fmt.Sprintf("kind は image または pdf のみ指定できます（指定値: %q）", kind),
		}
	}
	return nil
}

// validateProfileID は profileID が空文字でなく "/" を含まないことを確認する。
//
// Supabase sub は UUID 形式のため通常到達しないが、パストラバーサル的な
// prefix 構築の予防として防御的アサーションとして実施する。
func validateProfileID(profileID string) error {
	if profileID == "" {
		return fmt.Errorf("profileID が空です")
	}
	if strings.Contains(profileID, "/") {
		return fmt.Errorf("profileID に不正な文字 '/' が含まれています: %q", profileID)
	}
	return nil
}

// --- 昇格ヘルパー（event_command.go から呼ぶ）---

// isAllowedContentType は MIME タイプが画像または PDF の allowlist に含まれるか確認する。
func isAllowedContentType(ct string) bool {
	return allowedImageContentTypes[ct] || allowedPDFContentTypes[ct]
}

// isImageContentType は MIME タイプが画像 allowlist に含まれるか確認する。
func isImageContentType(ct string) bool {
	return allowedImageContentTypes[ct]
}

// isPDFContentType は MIME タイプが PDF allowlist に含まれるか確認する。
func isPDFContentType(ct string) bool {
	return allowedPDFContentTypes[ct]
}

// maxSizeByContentType は MIME タイプに対応する最大サイズを返す。
func maxSizeByContentType(ct string) int64 {
	if isImageContentType(ct) {
		return maxImageBytes
	}
	return maxPDFBytes
}

// sniffContentType は先頭バイトから実際の MIME タイプを判定する。
//
// PDF は先頭 5 バイトの "%PDF-" で判定する（http.DetectContentType が application/pdf を返さないため）。
// 画像は http.DetectContentType を使う。
func sniffContentType(data []byte) string {
	if len(data) >= 5 && bytes.HasPrefix(data, []byte("%PDF-")) {
		return "application/pdf"
	}
	// http.DetectContentType は最大 512 バイトを使う。
	return http.DetectContentType(data)
}

// stripEXIFAndReencode は JPEG/PNG 画像を再エンコードして EXIF/GPS 情報を除去する。
//
// Go 標準の image パッケージは EXIF を保持しないため、Decode → Encode で GPS 情報を含む
// すべてのメタデータを除去できる。CLAUDE.md geofuzzing ルールの座標保護に直結する処理。
func stripEXIFAndReencode(data []byte, contentType string) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("画像のデコードに失敗しました: %w", err)
	}

	var buf bytes.Buffer
	switch contentType {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("JPEG 再エンコードに失敗しました: %w", err)
		}
	case "image/png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("PNG 再エンコードに失敗しました: %w", err)
		}
	default:
		return nil, fmt.Errorf("未対応の画像 contentType: %q", contentType)
	}
	return buf.Bytes(), nil
}

// buildFinalKey は最終キーを生成する。
//
// destination には "events" または "reports" を指定する。
// 画像: natueve/{destination}/images/{uuid}{ext}
// PDF:  natueve/{destination}/documents/{uuid}{ext}
func buildFinalKey(destination, contentType string) string {
	ext := contentTypeToExt[contentType]
	id := uuid.New().String()

	if isImageContentType(contentType) {
		return fmt.Sprintf("natueve/%s/images/%s%s", destination, id, ext)
	}
	return fmt.Sprintf("natueve/%s/documents/%s%s", destination, id, ext)
}

// ownershipPrefix は profileID からオブジェクトキーの所有権 prefix を返す。
func ownershipPrefix(profileID string) string {
	return fmt.Sprintf("natueve/tmp/%s/", profileID)
}

// validateOwnership はオブジェクトキーが profileID の tmp 領域に属するか確認する。
// また、profileID が空文字でなく "/" を含まないことを防御的に検証する。
func validateOwnership(key, profileID string) error {
	if err := validateProfileID(profileID); err != nil {
		return fmt.Errorf("validateOwnership: %w", err)
	}
	prefix := ownershipPrefix(profileID)
	if !strings.HasPrefix(key, prefix) {
		return &ValidationError{
			Message: fmt.Sprintf("オブジェクトキー %q は自身の tmp 領域に属していません", key),
		}
	}
	return nil
}
