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

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// sample_meta.pdf に仕込んだ秘密文字列。昇格後の出力に残っていないことを確認する。
const (
	testMetaAuthor   = "Taro Yamada"
	testMetaSecret   = "Secret"
	testMetaXMP      = "xpacket"
	testMetaCreator  = "CreatorTool"
	testMetaDocument = "11111111-2222-3333-4444-555555555555"
)

// assertPDFPageCount は data のページ数が want であることを確認する。
func assertPDFPageCount(t *testing.T, data []byte, want int) {
	t.Helper()
	count, err := api.PageCount(context.Background(), bytes.NewReader(data), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatalf("ページ数の取得に失敗しました: %v", err)
	}
	if count != want {
		t.Errorf("ページ数: got %d, want %d", count, want)
	}
}

// assertNoMetadataStrings は data に文書情報・XMP 由来の文字列が残っていないことを確認する。
func assertNoMetadataStrings(t *testing.T, data []byte) {
	t.Helper()
	forbidden := []string{
		testMetaAuthor,
		testMetaSecret,
		testMetaXMP,
		testMetaCreator,
		testMetaDocument,
		"/Metadata",
		// pdfcpu 自身の指紋（PreserveInfoDict=true により付与されないはず）。
		"pdfcpu",
	}
	for _, s := range forbidden {
		if bytes.Contains(data, []byte(s)) {
			t.Errorf("メタデータ由来の文字列 %q が残っています", s)
		}
	}
}

func TestStripPDFMetadata(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{name: "正常: メタデータ付き PDF から文書情報と XMP を除去", input: loadTestdata(t, "sample_meta.pdf")},
		{name: "正常: メタデータ無しの PDF は pdfcpu の指紋を付与されない", input: loadTestdata(t, "sample.pdf")},
		{name: "異常: 破損した PDF", input: []byte("%PDF-1.4\nbroken content\n%%EOF\n"), wantErr: true},
		{name: "異常: 空のバイト列", input: []byte{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := stripPDFMetadata(context.Background(), tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			// 有効な PDF であり、ページ数が変わらないこと（1 ページ固定のテストデータ）。
			if !bytes.HasPrefix(out, []byte("%PDF-")) {
				t.Errorf("出力が PDF マジックナンバーで始まりません: %q", out[:min(len(out), 16)])
			}
			assertPDFPageCount(t, out, 1)
			assertNoMetadataStrings(t, out)
		})
	}
}

func TestStripPDFMetadata_RemovesSecretsFromInput(t *testing.T) {
	// テストデータ自体に秘密文字列が含まれることの確認（フィクスチャの健全性）。
	input := loadTestdata(t, "sample_meta.pdf")
	for _, s := range []string{testMetaAuthor, testMetaSecret, testMetaXMP, testMetaCreator, "/Metadata"} {
		if !bytes.Contains(input, []byte(s)) {
			t.Errorf("テストデータに期待した文字列 %q が含まれていません", s)
		}
	}
}

func TestPromoteOneObject_PDFMetadataStripped(t *testing.T) {
	data := loadTestdata(t, "sample_meta.pdf")
	key := "natueve/tmp/" + testProfileID + "/uuid.pdf"

	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "application/pdf",
		getData:         data,
	}

	finalKey, err := promoteOneObject(
		context.Background(),
		store,
		testProfileID,
		key,
		false,
		"資料.pdf",
		func(ct string) string { return buildFinalKey("events", ct) },
	)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	// Put 経路で保存されていること。
	if store.putKey != finalKey {
		t.Errorf("Put キー: got %q, want %q", store.putKey, finalKey)
	}
	if !strings.HasPrefix(finalKey, "natueve/events/documents/") {
		t.Errorf("最終キー prefix: got %q, want natueve/events/documents/", finalKey)
	}
	if store.putContentType != "application/pdf" {
		t.Errorf("Content-Type: got %q, want application/pdf", store.putContentType)
	}
	if store.putDisposition == "" {
		t.Error("Content-Disposition が空です")
	}

	// メタデータが除去され、ページ数が変わらないこと。
	assertNoMetadataStrings(t, store.putBody)
	assertPDFPageCount(t, store.putBody, 1)
}

func TestPromoteOneObject_PDFCorruptReturnsValidationError(t *testing.T) {
	corrupt := []byte("%PDF-1.4\nnot a real pdf\n")
	key := "natueve/tmp/" + testProfileID + "/uuid.pdf"

	store := &fakeObjectStore{
		headSize:        int64(len(corrupt)),
		headContentType: "application/pdf",
		getData:         corrupt,
	}

	_, err := promoteOneObject(
		context.Background(),
		store,
		testProfileID,
		key,
		false,
		"doc.pdf",
		func(ct string) string { return buildFinalKey("events", ct) },
	)
	// 破損 PDF は ValidationError で拒否され、Put も Delete も呼ばれないこと。
	_ = assertValidationError(t, err)
	if store.putCalls != 0 {
		t.Errorf("破損 PDF で Put が呼ばれました: calls=%d", store.putCalls)
	}
	if len(store.deleteKeys) != 0 {
		t.Errorf("破損 PDF で Delete が呼ばれました: keys=%v", store.deleteKeys)
	}
}

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
	// PNG: 0:8 signature / 8:12 IHDR length / 12:16 type / 16:29 data / 29:33 CRC.
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
