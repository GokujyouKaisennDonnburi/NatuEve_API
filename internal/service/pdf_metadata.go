package service

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// infoDictKeys は文書情報（Info 辞書）から除去する標準キー。
// 作成者・作成ツール・日付などの文書情報がこれに該当する（ADR-0046）。
var infoDictKeys = []string{
	"Title",
	"Author",
	"Subject",
	"Keywords",
	"Creator",
	"Producer",
	"CreationDate",
	"ModDate",
	"Trapped",
}

// stripPDFMetadata は PDF の文書情報（Info 辞書）と Catalog の /Metadata（XMP）を
// 除去したバイト列を返す。ページ内容・ページ数は変わらない。
//
// pdfcpu の低レベル API（ReadValidateAndOptimize → Info 辞書のキー削除 →
// Catalog /Metadata 削除 → Write）を使う。PreserveInfoDict=true により、書き出し時に
// pdfcpu が Producer と作成日時を再付与しない（ADR-0046）。
func stripPDFMetadata(data []byte) ([]byte, error) {
	conf := model.NewDefaultConfiguration()
	conf.PreserveInfoDict = true

	ctx, err := api.ReadValidateAndOptimize(context.Background(), bytes.NewReader(data), conf, nil)
	if err != nil {
		return nil, fmt.Errorf("PDF の解析に失敗しました: %w", err)
	}

	// Info 辞書が無い PDF では、pdfcpu の書き出しが Producer 等を新規付与するため
	// 空の Info 辞書を先に用意する（PDF 2.0 では Info 辞書は書き込まれない）。
	if ctx.Info == nil && ctx.XRefTable.Version() < model.V20 {
		ir, err := ctx.IndRefForNewObject(types.NewDict())
		if err != nil {
			return nil, fmt.Errorf("文書情報（Info）辞書の生成に失敗しました: %w", err)
		}
		ctx.Info = ir
	}
	if ctx.Info != nil {
		d, err := ctx.DereferenceDict(*ctx.Info)
		if err != nil {
			return nil, fmt.Errorf("文書情報（Info）辞書の参照に失敗しました: %w", err)
		}
		if d != nil {
			for _, k := range infoDictKeys {
				delete(d, k)
			}
			// カスタムプロパティのキーは UTF-16 エンコード名で格納されるため、
			// EncodeName 経由で削除する（pdfcpu removeAllProperties と同一の方法）。
			for k := range ctx.Properties {
				delete(d, types.EncodeName(k))
			}
		}
		ctx.Properties = map[string]string{}
	}

	root, err := ctx.Catalog()
	if err != nil {
		return nil, fmt.Errorf("文書カタログ（Catalog）の参照に失敗しました: %w", err)
	}
	delete(root, "Metadata")
	ctx.CatalogXMPMeta = nil

	var buf bytes.Buffer
	if err := api.Write(context.Background(), ctx, &buf, conf); err != nil {
		return nil, fmt.Errorf("PDF の書き出しに失敗しました: %w", err)
	}
	return buf.Bytes(), nil
}
