# ADR-0046: PDF 昇格時に文書情報と XMP メタデータを除去して保存する

- ステータス: Accepted
- 日付: 2026-11-12
- 関連: #279, #278, ADR-0045

## コンテキストと課題

イベント・レポートの PDF は昇格時に `store.Copy` でそのまま最終キーへ複製しており、
文書情報（作成者・作成ツール等）と XMP メタデータが最終オブジェクトにも残る。
公開ダウンロード可能な資料から作成者名等が漏れるため、保存前に除去する必要がある。
除去には PDF を解析・再書き出しするライブラリが必要で、新しい依存の選定
（ライセンス・保守状況）が課題。

## 決定

PDF 昇格を Copy から「取得済みデータを pdfcpu で解析し、文書情報（Info 辞書）と
Catalog の `/Metadata`（XMP）を除去して `store.Put` で保存する」方式に変える。
ライブラリは `github.com/pdfcpu/pdfcpu` v0.16.4（Apache-2.0）を採用する。

## 理由

- pdfcpu は Apache-2.0 で pure Go（CGO 不要）のため distroless コンテナで動く。
  2026-10 時点で活発に保守され、Go 1.26 に対応している。
- 高レベル API（`api.RemoveProperties`）は標準キー（Author/Creator/Producer 等）を
  消せず、書き出し時に pdfcpu 自身の Producer と日付を再付与する。そのため低レベル
  API（`ReadValidateAndOptimize` → Info 辞書のキー削除 → Catalog `/Metadata` 削除 →
  `Write`）と `PreserveInfoDict=true` を使う。Info 辞書が無い PDF では、書き出し時の
  新規付与を避けるため空の Info 辞書を先に用意する。
- 検討した他候補: unipdf は AGPL/商用、qpdf・exiftool は外部バイナリで distroless に
  載らない、自前パーサーは破損リスクが高い。それぞれ理由は下記の通り。

## 影響（結果）

### 良い影響

- 最終オブジェクトから作成者・作成ツール・XMP（dc:creator、xmp:CreatorTool 等）が消える。
- 既存の画像と同様に PDF も「取得・加工・保存」の経路に統一される。
- ObjectStore の `Copy` が不要になり、R2 実装とテストフェイクから削除できる。

### トレードオフ・制約

- 最大 20MB の PDF をメモリに載せる点は Copy 方式と同じ。pdfcpu の解析・最適化が
  一時メモリを追加で使う。
- 破損 PDF・暗号化 PDF（パスワード未提供）は解析に失敗し ValidationError になる
  （fail-closed）。利用者が資料を投稿できなくなるケースがあり得るが、メタデータ漏えい
  防止を優先する。
- pdfcpu が解析時に構造を最適化するため、出力バイト列は元と異なる
  （内容・ページ数は不変。テストで確認する）。

## 検討した代替案

- **unidoc/unipdf**: 機能は十分だが AGPL-3.0/商用。ライセンス要件が重いため不採用。
- **qpdf / exiftool を外部プロセスで呼ぶ**: 除去能力は十分だがコンテナへのバイナリ
  同梱が必要で distroless と相性が悪い。不採用。
- **自前で PDF を解析・再書き出し**: 依存を増やさないが、xref stream・object stream・
  増分更新・暗号化の対応が必要で破損リスクが高い。不採用。
- **高レベル API（api.RemoveProperties）のみ**: 標準 Info キーが残り、pdfcpu の
  Producer が再付与されるため目的を満たさない。不採用。
