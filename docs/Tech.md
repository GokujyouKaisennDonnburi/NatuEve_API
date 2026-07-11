## APIサーバー

Go (Gin) 製。Web / モバイルから呼び出される API サーバー。

### 認証

SupabaseAuth（Supabase が発行する JWT を JWKS で検証する想定。実装は今後）

### バックグラウンドワーカー（通知送信）

イベントキャンセル通知は Transactional Outbox（`event_notification_outbox` テーブル）経由で、
API サーバー内のワーカー goroutine が送信する（詳細は [ADR-0016](./adr/0016-event-cancel-notification-outbox.md)）。

> **運用上の前提: API サーバーは単一インスタンスで動かすこと。**
> ワーカーの outbox 取得は `FOR UPDATE SKIP LOCKED` による claim を行っていないため、
> 複数インスタンスへ横展開すると同じ通知を複数のワーカーが処理し、二重送信になる。
> 将来 AWS（App Runner / ECS 等）へ移行してスケールアウトする場合は、claim 方式の見直しが必要。


## 開発者向け 環境構築

開発・実行は **Docker 前提**。Docker さえあれば Go を直接インストールしなくても動かせる。

### 前提
- Docker / Docker Compose
- （Docker を使わずローカルで動かす場合のみ）Go 1.26.4 以上

### 環境変数
リポジトリ直下に `.env` を置く（`.env.example` をコピーして作成）。

```bash
cp .env.example .env
```

| 変数 | 説明 | デフォルト |
|---|---|---|
| `PORT` | サーバの待ち受けポート | `8085` |
| `TRUSTED_PROXIES` | 信頼するプロキシ（カンマ区切り CIDR/IP）。未設定なら全プロキシを信頼しない。本番でプロキシ越しに置く場合に設定 | （未設定） |
| `DATABASE_URL` | Postgres 接続文字列。未設定なら DB に接続せず起動する | （未設定） |
| `DB_AUTO_MIGRATE` | `true` で起動時にマイグレーションを自動適用（開発用）。本番は `false` | `false` |

> DB・マイグレーションの詳細（接続文字列の例・`make migrate-*`・本番での適用手順）は [Database.md](./Database.md) を参照。

依存ライブラリの一覧と用途は [dependencies.md](./dependencies.md) を参照。


## 動作確認

### 開発（Docker Compose）
ソースをマウントしてコンテナ内で [Air](https://github.com/air-verse/air) を実行する。
**ソースを編集すると自動で再ビルド・再起動される（ホットリロード）。** 設定は `.air.toml`。

```bash
make up        # 起動（中身は docker compose up。停止は Ctrl+C → make down）
```

> 初回起動時は Air の導入（バージョン固定）とビルドで少し時間がかかる。
> モジュール／ビルドキャッシュは named volume に永続化されるため、2 回目以降は速い。

別ターミナルでヘルスチェック:
```bash
curl http://localhost:8085/health
# => {"status":"ok"}
```

### 本番イメージ（Docker）
マルチステージビルドで軽量な distroless イメージを作る。

```bash
docker build -t NatuEve-api .
docker run -p 8085:8085 --env-file .env NatuEve-api
```

### Docker を使わない場合（任意）
```bash
go mod tidy          # 依存の取得・整理
go run ./cmd/api     # 起動
go build ./...       # ビルド確認のみ
go test ./...        # テスト
```

## API ドキュメント（Swagger）

ハンドラのアノテーションから OpenAPI を生成し、Swagger UI で確認できる。

- UI: サーバ起動後に `http://localhost:8085/swagger/index.html`
- 仕様の生成物: `api/`（`docs.go` / `swagger.json` / `swagger.yaml`）はコミット対象（手編集禁止）

アノテーション(ハンドラのコメントや `cmd/api/main.go` の `@title` 等)を変更したら再生成する:

```bash
make swag-install   # 初回のみ（swag CLI をバージョン固定で導入）
make swag           # api/ を再生成
```
