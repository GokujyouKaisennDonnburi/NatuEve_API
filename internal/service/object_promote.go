package service

import (
	"context"
	"fmt"
	"log/slog"
)

// promotedMedia は昇格後の最終キーと元ファイル名（同順）を保持する。
type promotedMedia struct {
	imageKeys  []string
	imageNames []string
	pdfKeys    []string
	pdfNames   []string
	// allKeys は補償削除用に画像・PDF の全最終キーを保持する。
	allKeys []string
}

// filenameAt は names[i] を返す。範囲外なら空文字を返す（ファイル名は任意のため）。
func filenameAt(names []string, i int) string {
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return ""
}

// promoteMedia は画像・PDF の tmp キー群を検証・洗浄して destination 領域へ一括昇格する。
//
// imageNames / pdfNames は対応するキーと同順の元ファイル名（範囲外・空は fallback）。
// 途中で失敗した場合は、それまでに昇格済みのオブジェクトを best-effort で削除してから返す。
func promoteMedia(
	ctx context.Context,
	store ObjectStore,
	profileID, destination string,
	imageKeys, imageNames, pdfKeys, pdfNames []string,
) (promotedMedia, error) {
	var pm promotedMedia
	buildKey := func(ct string) string { return buildFinalKey(destination, ct) }

	cleanup := func() {
		for _, k := range pm.allKeys {
			if delErr := store.Delete(ctx, k); delErr != nil {
				slog.Warn("昇格失敗時の補償削除に失敗しました", slog.String("key", k), slog.Any("error", delErr))
			}
		}
	}

	for i, key := range imageKeys {
		name := sanitizeFilename(filenameAt(imageNames, i))
		finalKey, err := promoteOneObject(ctx, store, profileID, key, true, name, buildKey)
		if err != nil {
			cleanup()
			return promotedMedia{}, err
		}
		pm.imageKeys = append(pm.imageKeys, finalKey)
		pm.imageNames = append(pm.imageNames, name)
		pm.allKeys = append(pm.allKeys, finalKey)
	}

	for i, key := range pdfKeys {
		name := sanitizeFilename(filenameAt(pdfNames, i))
		finalKey, err := promoteOneObject(ctx, store, profileID, key, false, name, buildKey)
		if err != nil {
			cleanup()
			return promotedMedia{}, err
		}
		pm.pdfKeys = append(pm.pdfKeys, finalKey)
		pm.pdfNames = append(pm.pdfNames, name)
		pm.allKeys = append(pm.allKeys, finalKey)
	}

	return pm, nil
}

// promoteOneObject は tmp 領域のオブジェクトを検証・洗浄して最終キーに配置する。
//
// buildKey は contentType から最終キーを生成する関数。
// promoteMedia は destination（"events" / "reports"）を固定した buildFinalKey を渡す。
//
// filename は元ファイル名。最終オブジェクトに Content-Disposition（inline）として
// 付与し、ダウンロード時に元名で保存されるようにする（空なら fallback 名を使う）。
func promoteOneObject(
	ctx context.Context,
	store ObjectStore,
	profileID, key string,
	isImage bool,
	filename string,
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
	disposition := contentDispositionInline(filename)

	if isImage {
		// EXIF/GPS 除去: 再エンコード（geofuzzing ルール準拠）
		clean, err := stripEXIFAndReencode(data, ct)
		if err != nil {
			return "", &ValidationError{
				Message: fmt.Sprintf("画像 %q の再エンコードに失敗しました: %v", key, err),
			}
		}
		if err := store.Put(ctx, finalKey, clean, ct, disposition); err != nil {
			return "", fmt.Errorf("画像の配置に失敗しました: %w", err)
		}
	} else {
		// PDF は Copy して昇格する。Content-Disposition を付け直すため ct も渡す。
		// PDF 内に埋め込まれた GPS 等のメタデータは今回スコープ外。
		if err := store.Copy(ctx, key, finalKey, ct, disposition); err != nil {
			return "", fmt.Errorf("PDFの配置に失敗しました: %w", err)
		}
	}

	return finalKey, nil
}
