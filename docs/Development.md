# 開発ガイド

実装者向けの規約と注意点をまとめる。環境構築・起動手順は [Tech.md](./Tech.md) を参照。

## コマンド（Makefile）

`make` または `make help` でタスク一覧が出る。

| コマンド | 用途 |
|---|---|
| `make run` | ローカルでサーバ起動（`go run ./cmd/api`） |
| `make build` | ビルド確認 |
| `make test` | テスト実行 |
| `make tidy` / `make fmt` / `make vet` | 依存整理 / 整形 / 静的解析 |
| `make swag-install` | swag CLI を導入（バージョン固定） |
| `make swag` | OpenAPI ドキュメント（`docs/`）を再生成 |
| `make swag-check` | `docs/` が最新か検証（CI用。差分があれば失敗） |
| `make up` / `make down` | 開発用コンテナの起動 / 停止 |

## ディレクトリ構成と責務

```
cmd/api/main.go        起動のみ（設定読込 → ルーター構築 → graceful shutdown）
internal/
  config/              環境変数 → Config 構造体
  server/              ルーター構築（NewRouter）・ミドルウェア登録・ルート定義
  middleware/          Gin ミドルウェア（ロギング・panic回復、今後: 認証/CORS）
  handler/             HTTP ハンドラ。入出力の変換のみ（ロジックを持たない）
  service/             ビジネスロジック（HTTP に依存しない）
  repository/          データアクセス。interface を定義し実装を分ける
  model/               ドメイン型・DTO
docs/                  swag 生成物（docs.go / swagger.json|yaml）
```

**依存方向は `handler → service → repository` の一方向**。逆流させない。
`repository` は interface で定義し、`service` のテスト時にモックへ差し替えられるようにする。

> `service` / `repository` / `model` は枠（doc.go）のみ。最初のデータ機能を追加するときに実装する。
> 空のまま増やさず、必要になった層から埋めること。

## コミット規約

種別プレフィックス + 末尾に対象 Issue 番号 `#番号`。

| 種別 | 用途 |
|---|---|
| `feat:` | 機能追加 |
| `update:` | 機能変更 |
| `fix:` | バグ修正 |
| `docs:` | ドキュメント修正 |
| `refactor:` | リファクタリング |
| `test:` | テスト |

例: `docs: githubのドキュメント作成 #3`

1 つの作業は種別ごとに分けてコミットする。Issue 番号は作業ブランチ名（例 `issue/1` → `#1`）から判断する。

## ログ方針

- 標準 `log` ではなく **`log/slog`（Go 標準）で構造化ログ（JSON）** を出す。外部ライブラリは使わない。
- `main.go` で `slog.SetDefault` し、JSON ハンドラを既定にする。
- Gin のアクセスログも slog に揃えるため、`gin.Default()` は使わず **`gin.New()` + `middleware.SlogLogger()` / `middleware.SlogRecovery()`** を使う。
- 本番では `GIN_MODE=release` を設定する（起動時のデバッグ出力を抑制）。

## API ドキュメント（Swagger）

- ハンドラのアノテーション（コメント）や `cmd/api/main.go` の `@title` 等から生成する。
- **アノテーションを変更したら必ず `make swag` で `docs/` を再生成してコミットする。**
- `docs/` はリポジトリにコミットする運用。CI の `make swag-check` が再生成漏れを検知して落とす。
- swag のバージョンは `Makefile` の `SWAG_VERSION` で固定（go.mod の `swaggo/swag` と一致させる）。
- UI: サーバ起動後に `http://localhost:8080/swagger/index.html`。

## CI（GitHub Actions）

`main` への push と全 PR で `.github/workflows/ci.yml` が動く。実行内容:

`make swag-check`（docs 更新漏れ） → `make vet` → `make build` → `make test`

ローカルでも同じ `make` ターゲットで再現できる。push 前に `make swag-check vet build test` を回すと CI を落としにくい。

## 実装時の注意点

- **機密情報を `.env` にコミットしない**。`.env` は `.gitignore` 済み、共有はキー名のみ `.env.example` で行う。本番は実行環境の環境変数 / シークレットマネージャで注入する（[.env.example](../.env.example) 参照）。
- **`SetTrustedProxies`**: 未設定（nil）はどのプロキシも信頼しない。プロキシ越しに置く場合のみ `TRUSTED_PROXIES` に CIDR を設定する。
- **認証**: Supabase が発行する JWT を JWKS で検証する想定。実装時は `internal/middleware/` に追加する。
