package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func encodePromotionTestImage(t *testing.T, contentType string, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	var err error
	switch contentType {
	case "image/jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	case "image/png":
		err = png.Encode(&buf, img)
	default:
		t.Fatalf("未対応の Content-Type: %q", contentType)
	}
	if err != nil {
		t.Fatalf("テスト画像の生成に失敗: %v", err)
	}
	return buf.Bytes()
}

func pngConfigData(t *testing.T, width, height int) []byte {
	t.Helper()

	data := encodePromotionTestImage(t, "image/png", 1, 1)
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data[:33]
}

func TestValidateImagePixelSize_Boundary(t *testing.T) {
	if err := validateImagePixelSize(pngConfigData(t, maxImagePixels, maxImagePixels)); err != nil {
		t.Fatalf("上限ちょうどの画像でエラー: %v", err)
	}

	err := validateImagePixelSize(pngConfigData(t, maxImagePixels+1, maxImagePixels))
	if err == nil {
		t.Fatal("上限を1px超える画像でエラーを期待したが nil だった")
	}
}

func TestPromoteOneObject_ImagePixelLimit(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		width       int
		height      int
		wantErr     bool
	}{
		{
			name:        "正常: PNG の幅が上限ちょうど",
			contentType: "image/png",
			width:       maxImagePixels,
			height:      1,
		},
		{
			name:        "正常: JPEG の高さが上限ちょうど",
			contentType: "image/jpeg",
			width:       1,
			height:      maxImagePixels,
		},
		{
			name:        "異常: PNG の幅が上限を1px超える",
			contentType: "image/png",
			width:       maxImagePixels + 1,
			height:      1,
			wantErr:     true,
		},
		{
			name:        "異常: JPEG の高さが上限を1px超える",
			contentType: "image/jpeg",
			width:       1,
			height:      maxImagePixels + 1,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := encodePromotionTestImage(t, tt.contentType, tt.width, tt.height)
			store := &fakeObjectStore{
				headSize:        int64(len(data)),
				headContentType: tt.contentType,
				getData:         data,
			}

			_, err := promoteOneObject(
				context.Background(),
				store,
				"profile-001",
				"natueve/tmp/profile-001/image",
				true,
				"image",
				func(string) string { return "natueve/events/images/final" },
			)

			if tt.wantErr {
				validationErr := assertValidationError(t, err)
				if !strings.Contains(validationErr.Message, "上限") {
					t.Errorf("エラーメッセージに上限超過が含まれていません: %q", validationErr.Message)
				}
				if store.putCalls != 0 {
					t.Errorf("上限超過時に Put が呼ばれました: %d", store.putCalls)
				}
				return
			}

			assertNoErr(t, err)
			if store.putCalls != 1 {
				t.Errorf("Put 呼び出し回数: got %d, want 1", store.putCalls)
			}
		})
	}
}

func TestPromoteOneObject_ImagePixelLimitCheckedBeforeFullDecode(t *testing.T) {
	// IHDR だけのため、DecodeConfig は寸法を読めるが、全体の Decode は失敗する。
	data := pngConfigData(t, maxImagePixels+1, 1)
	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "image/png",
		getData:         data,
	}

	_, err := promoteOneObject(
		context.Background(),
		store,
		"profile-001",
		"natueve/tmp/profile-001/image.png",
		true,
		"image.png",
		func(string) string { return "natueve/events/images/final.png" },
	)

	validationErr := assertValidationError(t, err)
	if !strings.Contains(validationErr.Message, "8001x1") {
		t.Errorf("全体デコードより前に寸法超過を検出できていません: %q", validationErr.Message)
	}
	if store.putCalls != 0 {
		t.Errorf("上限超過時に Put が呼ばれました: %d", store.putCalls)
	}
}

func TestPromoteOneObject_ImageDecodeConfigError(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "image/png",
		getData:         data,
	}

	_, err := promoteOneObject(
		context.Background(),
		store,
		"profile-001",
		"natueve/tmp/profile-001/broken.png",
		true,
		"broken.png",
		func(string) string { return "natueve/events/images/final.png" },
	)

	validationErr := assertValidationError(t, err)
	if !strings.Contains(validationErr.Message, "寸法取得に失敗") {
		t.Errorf("DecodeConfig のエラーが ValidationError に変換されていません: %q", validationErr.Message)
	}
	if store.putCalls != 0 {
		t.Errorf("壊れた画像で Put が呼ばれました: %d", store.putCalls)
	}
}
