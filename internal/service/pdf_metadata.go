package service

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// stripPDFMetadata は PDF の文書情報（Info 辞書）と Catalog の /Metadata（XMP）を
// 除去したバイト列を返す。ページ内容・ページ数は変わらない。
//
// pdfcpu の低レベル API（ReadValidateAndOptimize → Info 辞書のクリア → Catalog
// /Metadata 削除 → Write）を使う。PreserveInfoDict=true により、書き出し時に
// pdfcpu が Producer と作成日時を再付与しない（ADR-0046）。
func stripPDFMetadata(ctx context.Context, data []byte) ([]byte, error) {
	// NewStatelessConfiguration を使う。NewDefaultConfiguration は初回呼び出し時に
	// $HOME 配下へ config.yml 等の書き込みを試み、distroless 等の書き込み不可環境で
	// panic するため（ADR-0046）。
	conf := model.NewStatelessConfiguration()
	conf.PreserveInfoDict = true

	pdfCtx, err := api.ReadValidateAndOptimize(ctx, bytes.NewReader(data), conf, nil)
	if err != nil {
		return nil, fmt.Errorf("PDF の解析に失敗しました: %w", err)
	}

	// Info 辞書が無い PDF では、pdfcpu の書き出しが Producer 等を新規付与するため
	// 空の Info 辞書を先に用意する（PDF 2.0 では Info 辞書は書き込まれない）。
	if pdfCtx.Info == nil && pdfCtx.XRefTable.Version() < model.V20 {
		ir, err := pdfCtx.IndRefForNewObject(types.NewDict())
		if err != nil {
			return nil, fmt.Errorf("文書情報（Info）辞書の生成に失敗しました: %w", err)
		}
		pdfCtx.Info = ir
	}
	if pdfCtx.Info != nil {
		d, err := pdfCtx.DereferenceDict(*pdfCtx.Info)
		if err != nil {
			return nil, fmt.Errorf("文書情報（Info）辞書の参照に失敗しました: %w", err)
		}
		// 標準キー・カスタムプロパティ・AAPL:Keywords 等の規格外エントリを問わず
		// 全エントリを除去する。キー列挙による個別削除では取りこぼすため。
		clear(d)
		pdfCtx.Properties = map[string]string{}
	}

	root, err := pdfCtx.Catalog()
	if err != nil {
		return nil, fmt.Errorf("文書カタログ（Catalog）の参照に失敗しました: %w", err)
	}
	delete(root, "Metadata")
	pdfCtx.CatalogXMPMeta = nil

	var buf bytes.Buffer
	if err := api.Write(ctx, pdfCtx, &buf, conf); err != nil {
		return nil, fmt.Errorf("PDF の書き出しに失敗しました: %w", err)
	}
	return buf.Bytes(), nil
}
