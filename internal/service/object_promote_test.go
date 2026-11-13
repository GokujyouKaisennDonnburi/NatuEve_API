package service

import (
	"bytes"
	"context"
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
