package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

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

func pngConfigData(t *testing.T, width, height uint32) []byte {
	t.Helper()

	data := encodePromotionTestImage(t, "image/png", 1, 1)
	// PNG: 0:8 signature / 8:12 IHDR length / 12:16 type / 16:29 data / 29:33 CRC.
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data[:33]
}

// pngConfigData16bit は IHDR のビット深度を 16 に書き換えた PNG ヘッダを返す。
// DecodeConfig は 16 ビット形式の ColorModel を報告するが、本体がないため全体の Decode は失敗する。
func pngConfigData16bit(t *testing.T, width, height uint32) []byte {
	t.Helper()

	data := pngConfigData(t, width, height)
	data[24] = 16 // bit depth
	data[25] = 6  // color type: truecolor + alpha
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// appendPNGChunk は dst に PNG チャンク（length + type + data + CRC）を追記する。
// chunkLength は chunkData のバイト数。テストで使うチャンクは小さいため、
// 長さは定数で渡す。
func appendPNGChunk(dst []byte, chunkType string, chunkLength uint32, chunkData []byte) []byte {
	var head [8]byte
	binary.BigEndian.PutUint32(head[0:4], chunkLength)
	copy(head[4:8], chunkType)
	dst = append(dst, head[:]...)
	dst = append(dst, chunkData...)

	crcInput := append([]byte(chunkType), chunkData...)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(crcInput))
	return append(dst, crc[:]...)
}

// pngGrayHeaderData は グレースケール（color type 0）の IHDR と、withTRNS が true なら
// tRNS チャンクからなる PNG データを返す。本体（IDAT）を持たないため、DecodeConfig は
// 読めるが全体の Decode は失敗する。
func pngGrayHeaderData(t *testing.T, width, height uint32, bitDepth byte, withTRNS bool) []byte {
	t.Helper()

	data := append([]byte(nil), pngSignature...)
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = bitDepth
	ihdr[9] = 0 // color type: グレースケール
	data = appendPNGChunk(data, "IHDR", 13, ihdr)
	if withTRNS {
		data = appendPNGChunk(data, "tRNS", 2, []byte{0, 0})
	}
	return data
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

func TestEstimatedBytesPerPixel(t *testing.T) {
	tests := []struct {
		name  string
		model color.Model
		want  int
	}{
		{name: "8ビット グレースケールは 1 byte", model: color.GrayModel, want: 1},
		{name: "16ビット グレースケールは 2 bytes", model: color.Gray16Model, want: 2},
		{name: "8ビット RGBA は 4 bytes", model: color.RGBAModel, want: 4},
		{name: "8ビット NRGBA は 4 bytes", model: color.NRGBAModel, want: 4},
		{name: "YCbCr は 4 bytes", model: color.YCbCrModel, want: 4},
		{name: "CMYK は 4 bytes", model: color.CMYKModel, want: 4},
		{name: "16ビット RGBA64 は 8 bytes", model: color.RGBA64Model, want: 8},
		{name: "16ビット NRGBA64 は 8 bytes", model: color.NRGBA64Model, want: 8},
		{name: "パレットは 1 byte", model: color.Palette{}, want: 1},
		{name: "未知の形式は保守的に 8 bytes", model: color.AlphaModel, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimatedBytesPerPixel(tt.model); got != tt.want {
				t.Errorf("estimatedBytesPerPixel: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateImagePixelSize_DecodedBytesLimit(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name: "正常: 8ビット RGBA の 8000x8000 は寸法上限内",
			data: pngConfigData(t, maxImagePixels, maxImagePixels),
		},
		{
			name: "正常: 16ビット PNG の 4000x4000 は展開後 128MB で上限内",
			data: pngConfigData16bit(t, 4000, 4000),
		},
		{
			name:    "異常: 16ビット PNG の 8000x8000 は展開後 512MB で上限超過",
			data:    pngConfigData16bit(t, maxImagePixels, maxImagePixels),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImagePixelSize(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				if !strings.Contains(err.Error(), "展開後サイズ") {
					t.Errorf("エラーメッセージに展開後サイズ超過が含まれていません: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
		})
	}
}

func TestPromoteOneObject_16bitImageWithinLimitAccepted(t *testing.T) {
	// png.Encode は RGBA64 を 16 ビット PNG として出力する。
	img := image.NewRGBA64(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("16ビット PNG の生成に失敗: %v", err)
	}
	data := buf.Bytes()

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("テスト画像の DecodeConfig に失敗: %v", err)
	}
	if got := estimatedBytesPerPixel(config.ColorModel); got != 8 {
		t.Fatalf("テスト画像が 16 ビットとして見積もられていません: %d bytes/pixel", got)
	}

	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "image/png",
		getData:         data,
	}

	_, err = promoteOneObject(
		context.Background(),
		store,
		"profile-001",
		"natueve/tmp/profile-001/image.png",
		true,
		"image.png",
		func(string) string { return "natueve/events/images/final.png" },
	)

	assertNoErr(t, err)
	if store.putCalls != 1 {
		t.Errorf("Put 呼び出し回数: got %d, want 1", store.putCalls)
	}
}

func TestPromoteOneObject_DecodedBytesLimitCheckedBeforeFullDecode(t *testing.T) {
	// IHDR だけのため、DecodeConfig は寸法と色形式を読めるが、全体の Decode は失敗する。
	// 展開後サイズの超過を全体デコードより前に検出できることを確認する。
	data := pngConfigData16bit(t, maxImagePixels, maxImagePixels)
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
	if !strings.Contains(validationErr.Message, "展開後サイズ") {
		t.Errorf("全体デコードより前に展開後サイズ超過を検出できていません: %q", validationErr.Message)
	}
	if store.putCalls != 0 {
		t.Errorf("上限超過時に Put が呼ばれました: %d", store.putCalls)
	}
}

func TestPNGHasTRNS(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "tRNS 付きグレースケール PNG", data: pngGrayHeaderData(t, 1, 1, 8, true), want: true},
		{name: "tRNS 無しのグレースケール PNG", data: pngGrayHeaderData(t, 1, 1, 8, false)},
		{name: "tRNS 無しの RGBA PNG", data: pngConfigData(t, 1, 1)},
		{name: "PNG 以外", data: encodePromotionTestImage(t, "image/jpeg", 1, 1)},
		{name: "空のバイト列", data: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pngHasTRNS(tt.data); got != tt.want {
				t.Errorf("pngHasTRNS: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateImagePixelSize_GrayscaleTRNS(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name: "正常: tRNS 無しの 16ビット グレースケール 8000x8000 は展開後 128MB で上限内",
			data: pngGrayHeaderData(t, maxImagePixels, maxImagePixels, 16, false),
		},
		{
			name: "正常: tRNS 付き 8ビット グレースケール 8000x8000 は NRGBA 展開の 256MB で上限内",
			data: pngGrayHeaderData(t, maxImagePixels, maxImagePixels, 8, true),
		},
		{
			name: "正常: tRNS 付き 16ビット グレースケール 4000x4000 は NRGBA64 展開の 128MB で上限内",
			data: pngGrayHeaderData(t, 4000, 4000, 16, true),
		},
		{
			name:    "異常: tRNS 付き 16ビット グレースケール 8000x8000 は NRGBA64 展開の 512MB で上限超過",
			data:    pngGrayHeaderData(t, maxImagePixels, maxImagePixels, 16, true),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImagePixelSize(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				if !strings.Contains(err.Error(), "展開後サイズ") {
					t.Errorf("エラーメッセージに展開後サイズ超過が含まれていません: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
		})
	}
}

func TestPromoteOneObject_GrayscaleTRNSLimitCheckedBeforeFullDecode(t *testing.T) {
	// tRNS 付きグレースケールは DecodeConfig より多く展開されるため、
	// 全体デコードより前に展開後サイズ超過を検出できることを確認する。
	data := pngGrayHeaderData(t, maxImagePixels, maxImagePixels, 16, true)
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
	if !strings.Contains(validationErr.Message, "展開後サイズ") {
		t.Errorf("全体デコードより前に展開後サイズ超過を検出できていません: %q", validationErr.Message)
	}
	if store.putCalls != 0 {
		t.Errorf("上限超過時に Put が呼ばれました: %d", store.putCalls)
	}
}

func TestPromoteOneObject_Grayscale16WithTRNSAccepted(t *testing.T) {
	// png.Encode の出力（gray16）の IHDR 直後に tRNS チャンクを挿入する。
	img := image.NewGray16(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("16ビット グレースケール PNG の生成に失敗: %v", err)
	}
	// PNG: 0:8 signature / 8:12 IHDR length / 12:16 type / 16:29 data / 29:33 CRC / 33: tRNS
	data := appendPNGChunk(buf.Bytes()[:33:33], "tRNS", 2, []byte{0, 0})
	data = append(data, buf.Bytes()[33:]...)

	// 実際に Decode しても NRGBA64 として展開される（ColorModel の見積もりより大きい）。
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("テスト画像の Decode に失敗: %v", err)
	}
	if _, ok := m.(*image.NRGBA64); !ok {
		t.Fatalf("tRNS 付き 16ビット グレースケールが NRGBA64 で展開されていません: %T", m)
	}

	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "image/png",
		getData:         data,
	}

	_, err = promoteOneObject(
		context.Background(),
		store,
		"profile-001",
		"natueve/tmp/profile-001/image.png",
		true,
		"image.png",
		func(string) string { return "natueve/events/images/final.png" },
	)

	assertNoErr(t, err)
	if store.putCalls != 1 {
		t.Errorf("Put 呼び出し回数: got %d, want 1", store.putCalls)
	}
}

func TestPromoteOneObject_ConcurrentDecodeLimited(t *testing.T) {
	data := encodePromotionTestImage(t, "image/png", 2, 2)
	store := &fakeObjectStore{
		headSize:        int64(len(data)),
		headContentType: "image/png",
		getData:         data,
	}

	// 失敗時にスロットが残って後続のテストに影響しないよう、終了時に空にする。
	t.Cleanup(func() {
		for {
			select {
			case <-imageDecodeSlots:
			default:
				return
			}
		}
	})

	// セマフォを満杯にする。
	for i := 0; i < maxConcurrentImageDecodes; i++ {
		imageDecodeSlots <- struct{}{}
	}

	done := make(chan error, 1)
	go func() {
		_, err := promoteOneObject(
			context.Background(),
			store,
			"profile-001",
			"natueve/tmp/profile-001/image.png",
			true,
			"image.png",
			func(string) string { return "natueve/events/images/final.png" },
		)
		done <- err
	}()

	// スロットが満杯の間は展開が始まらず、処理も完了しない。
	select {
	case err := <-done:
		t.Fatalf("セマフォが満杯でも処理が完了しました: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if store.putCalls != 0 {
		t.Errorf("セマフォ待ちの間に Put が呼ばれました: %d", store.putCalls)
	}

	// スロットを解放すると処理が進行する。
	<-imageDecodeSlots
	select {
	case err := <-done:
		assertNoErr(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("スロット解放後も処理が完了しません")
	}
	if store.putCalls != 1 {
		t.Errorf("Put 呼び出し回数: got %d, want 1", store.putCalls)
	}
}
