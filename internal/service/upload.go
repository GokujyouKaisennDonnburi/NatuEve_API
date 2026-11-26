package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
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
	PresignPut(ctx context.Context, key, contentType string, size int64) (url string, expiresAt time.Time, err error)
	// Head はオブジェクトのメタデータ（サイズ・Content-Type）を返す。
	Head(ctx context.Context, key string) (size int64, contentType string, err error)
	// Get はオブジェクトの全バイトを返す。maxBytes を超えるオブジェクトはエラー。
	Get(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	// Put は body を指定キーで PUT する。
	// contentDisposition が空でなければ Content-Disposition として保存する。
	Put(ctx context.Context, key string, body []byte, contentType, contentDisposition string) error
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
	// maxImagePixels は画像の幅・高さそれぞれの最大ピクセル数。Web 側の上限と揃える。
	maxImagePixels = 8000
	// maxImageDecodedBytes は画像の展開後サイズの推定値の上限（256MiB）。
	// 8000×8000px の RGBA 展開（256,000,000 bytes）は上限内で受け付ける。
	maxImageDecodedBytes int64 = 256 << 20
	// maxConcurrentImageDecodes は画像の展開・再エンコードを同時に実行する数の上限。
	// 上限の根拠は ADR-0047。
	maxConcurrentImageDecodes = 1
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
func (s *UploadService) PresignPut(ctx context.Context, profileID, kind, contentType string, size int64) (model.PresignResponse, error) {
	if err := validateProfileID(profileID); err != nil {
		return model.PresignResponse{}, err
	}

	if err := validateKindContentType(kind, contentType); err != nil {
		return model.PresignResponse{}, err
	}

	if err := validateUploadSize(contentType, size); err != nil {
		return model.PresignResponse{}, err
	}

	ext := contentTypeToExt[contentType]
	key := fmt.Sprintf("natueve/tmp/%s/%s%s", profileID, uuid.New().String(), ext)

	uploadURL, expiresAt, err := s.store.PresignPut(ctx, key, contentType, size)
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

// validateUploadSize は Content-Type に対応したアップロードサイズ上限を検証する
func validateUploadSize(contentType string, size int64) error {
	if size <= 0 {
		return &ValidationError{
			Message: "ファイルサイズは1 byte以上を指定してください",
		}
	}

	maxBytes := maxSizeByContentType(contentType)

	if maxBytes == 0 {
		return &ValidationError{
			Message: fmt.Sprintf("未対応の Content-Type です（指定値: %q）", contentType),
		}
	}

	// 上限を超えている場合はエラーにする
	if size > maxBytes {
		return &ValidationError{
			Message: fmt.Sprintf(
				"ファイルサイズが上限を超えています（最大 %d bytes、指定値 %d bytes）",
				maxBytes,
				size,
			),
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
	if isPDFContentType(ct) {
		return maxPDFBytes
	}
	return 0
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

// validateImagePixelSize は画像全体を復元せず、ヘッダから幅・高さと
// 展開後サイズの推定値を検証する。
func validateImagePixelSize(data []byte) error {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("画像の寸法取得に失敗しました: %w", err)
	}
	if config.Width > maxImagePixels || config.Height > maxImagePixels {
		return fmt.Errorf(
			"画像の縦横ピクセル数(%dx%d)が上限(%dx%d)を超えています",
			config.Width,
			config.Height,
			maxImagePixels,
			maxImagePixels,
		)
	}
	return validateImageDecodedBytes(config, pngHasTRNS(data))
}

// pngSignature は PNG のシグネチャ。
var pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// pngHasTRNS は data が tRNS チャンクを含む PNG かどうか返す。PNG でない場合は false を返す。
//
// チャンク構造のなぞりだけを行い、内容の検証はしない。壊れた PNG は後続の Decode で拒否される。
func pngHasTRNS(data []byte) bool {
	if !bytes.HasPrefix(data, pngSignature) {
		return false
	}
	// チャンクは length(4) + type(4) + data(length) + CRC(4)。
	for off := int64(8); off+8 <= int64(len(data)); {
		length := int64(binary.BigEndian.Uint32(data[off : off+4]))
		if bytes.Equal(data[off+4:off+8], []byte("tRNS")) {
			return true
		}
		off += 12 + length
	}
	return false
}

// validateImageDecodedBytes は幅×高さ×1ピクセルあたりのバイト数から
// 展開後サイズを推定し、上限を超える場合はエラーを返す。
//
// hasTRNS は PNG が tRNS チャンクを含むかどうか。
func validateImageDecodedBytes(config image.Config, hasTRNS bool) error {
	bpp := estimatedBytesPerPixel(config.ColorModel)
	// tRNS 付きグレースケールは NRGBA/NRGBA64 として展開され、
	// DecodeConfig の ColorModel より多くのメモリを使う。
	if hasTRNS {
		switch config.ColorModel {
		case color.GrayModel:
			bpp = 4
		case color.Gray16Model:
			bpp = 8
		}
	}
	estimated := int64(config.Width) * int64(config.Height) * int64(bpp)
	if estimated > maxImageDecodedBytes {
		return fmt.Errorf(
			"画像の展開後サイズの見積もり(%d x %d x %d bytes = %d bytes)が上限(%d bytes)を超えています",
			config.Width,
			config.Height,
			bpp,
			estimated,
			maxImageDecodedBytes,
		)
	}
	return nil
}

// estimatedBytesPerPixel は DecodeConfig の ColorModel から、標準パッケージでの
// 展開後の 1 ピクセルあたりのバイト数の上限を返す。
//
// 16 ビット形式（RGBA64/NRGBA64/Gray16）はそれぞれ 8/8/2 バイトで展開される。
// YCbCr はサブサンプリングにより最大 3 バイトだが、保守的に 4 を返す。
// 未知の形式は保守的に 8 を返す。
func estimatedBytesPerPixel(m color.Model) int {
	switch m {
	case color.GrayModel:
		return 1
	case color.Gray16Model:
		return 2
	case color.RGBAModel, color.NRGBAModel, color.YCbCrModel, color.NYCbCrAModel, color.CMYKModel:
		return 4
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default:
		if _, ok := m.(color.Palette); ok {
			return 1
		}
		return 8
	}
}

// buildFinalKey は最終キーを生成する。
//
// destination には "events" または "reports" を指定する。
// 画像: natueve/{destination}/images/{uuid}{ext}
// PDF:  natueve/{destination}/documents/{uuid}{ext}
//
// 画像のキーの形式を変えるときは、フロントの next.config.ts の images.remotePatterns の
// pathname も合わせて変える（docs/Deployment.md）。
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
