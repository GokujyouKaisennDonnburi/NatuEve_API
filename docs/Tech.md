## APIサーバー

### 認証

SpabaseAuth


## 開発者向け 環境構築

### 前提
- Go 1.26.4 

### セットアップ
```bash
# 依存をダウンロード・整理
go mod tidy
```

依存ライブラリの一覧と用途は [dependencies.md](./dependencies.md) を参照。

### 環境変数
開発時はリポジトリ直下に `.env` を置く（無ければ環境変数をそのまま使用）。

| 変数 | 説明 | デフォルト |
|---|---|---|
| `PORT` | サーバの待ち受けポート | `8080` |

## 動作確認

```bash
# サーバ起動
go run .
```

別ターミナルでヘルスチェック:
```bash
curl http://localhost:8080/health
# => {"status":"ok"}
```

ビルドのみ確認する場合:
```bash
go build ./...
```
