package service

import (
	"context"
	"fmt"
)

// promoteOneObject は tmp 領域のオブジェクトを検証・洗浄して最終キーに配置する。
//
// buildKey は最終キーを生成する関数。ドメインごとに異なるパスを渡す。
// 例: イベント → buildFinalKey、レポート → buildReportFinalKey
func promoteOneObject(
	ctx context.Context,
	store ObjectStore,
	profileID, key string,
	isImage bool,
	buildKey func(contentType string) string,
) (string, error) {
	if err := validateOwnership(key, profileID); err != nil {
		return "", err
	}

	size, ct, err := store.Head(ctx, key)
	if err != nil {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q のメタデータ取得に失敗しました: %v", key, err),
		}
	}

	if !isAllowedContentType(ct) {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q の Content-Type %q は許可されていません", key, ct),
		}
	}

	maxSize := maxSizeByContentType(ct)
	if size > maxSize {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q のサイズ(%d bytes)が上限(%d bytes)を超えています", key, size, maxSize),
		}
	}

	if isImage && !isImageContentType(ct) {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q は画像として指定されましたが Content-Type が %q です", key, ct),
		}
	}
	if !isImage && !isPDFContentType(ct) {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q は PDF として指定されましたが Content-Type が %q です", key, ct),
		}
	}

	// Head で確認済みの maxSize を渡し、TOCTOU による巨大ファイルのメモリ展開を防ぐ。
	data, err := store.Get(ctx, key, maxSize)
	if err != nil {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q の取得に失敗しました: %v", key, err),
		}
	}

	detected := sniffContentType(data)
	if !isSameContentTypeFamily(ct, detected) {
		return "", &ValidationError{
			Message: fmt.Sprintf("オブジェクト %q の実体(%q)が宣言 Content-Type(%q) と一致しません", key, detected, ct),
		}
	}

	finalKey := buildKey(ct)

	if isImage {
		// EXIF/GPS 除去: 再エンコード（geofuzzing ルール準拠）
		clean, err := stripEXIFAndReencode(data, ct)
		if err != nil {
			return "", &ValidationError{
				Message: fmt.Sprintf("画像 %q の再エンコードに失敗しました: %v", key, err),
			}
		}
		if err := store.Put(ctx, finalKey, clean, ct); err != nil {
			return "", fmt.Errorf("画像の配置に失敗しました: %w", err)
		}
	} else {
		// PDF は Copy して昇格する。
		// PDF 内に埋め込まれた GPS 等のメタデータは今回スコープ外。
		if err := store.Copy(ctx, key, finalKey); err != nil {
			return "", fmt.Errorf("PDFの配置に失敗しました: %w", err)
		}
	}

	return finalKey, nil
}
