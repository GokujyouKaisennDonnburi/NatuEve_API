## APIサーバー

Go (Gin) 製。Web / モバイルから呼び出される API サーバー。

### 認証

SupabaseAuth（Supabase が発行する JWT を JWKS で検証する想定。実装は今後）

### バックグラウンドワーカー（通知送信）

イベントキャンセル・欠席連絡の通知は Transactional Outbox（`event_notification_outbox` テーブル）経由で送信する
（詳細は [ADR-0016](./adr/0016-event-cancel-notification-outbox.md)、[ADR-0031](./adr/0031-absence-notification.md)）。

- **本番（Cloud Run）**: Cloud Scheduler が 1 分間隔で `POST /internal/outbox/process` を呼び、1 回の呼び出しで未送信の行を最大 50 件処理する。
  認証は Scheduler の OIDC トークン（[ADR-0035](./adr/0035-outbox-scheduler-endpoint.md)）。常駐ワーカーは使わない。
  設定は [Deployment.md](./Deployment.md) を参照。
- **ローカル開発**: 環境変数 `OUTBOX_WORKER_ENABLED=true` のとき、API プロセス内のワーカーが 30 秒間隔で同じ処理を呼ぶ。
- **排他**: 行は `FOR UPDATE SKIP LOCKED` で 1 件ずつ確保し、送信結果の記録と同じトランザクションで確定する。
  複数の呼び出しやインスタンスが同時に走っても、同じ通知を同時に送ることはない。ただし、送信後に記録できなかった場合と、送信を打ち切ったが実際には届いていた場合は再送されうる（at-least-once）。
- Resend 未設定（`RESEND_API_KEY`・`MAIL_FROM` が無い）の場合は、内部エンドポイントもワーカーも動かず、通知は outbox に溜まる。


## 開発者向け 環境構築

開発・実行は **Docker 前提**。Docker さえあれば Go を直接インストールしなくても動かせる。

### 前提
- Docker / Docker Compose
- （Docker を使わずローカルで動かす場合のみ）Go 1.26.10 以上

### 環境変数
リポジトリ直下に `.env` を置く（`.env.example` をコピーして作成）。

```bash
cp .env.example .env
```

| 変数 | 説明 | デフォルト |
|---|---|---|
| `PORT` | サーバの待ち受けポート | `8085` |
| `TRUSTED_PROXIES` | 信頼するプロキシ（カンマ区切り CIDR/IP）。未設定なら全プロキシを信頼しない。本番では使わない（利用者 IP は `RELAY_CLIENT_IP_HEADER` から読む。ADR-0034） | （未設定） |
| `RELAY_SECRET` | 中継用 Worker が秘密ヘッダー `X-Relay-Auth` に入れる値（32 バイト以上）。`RELAY_CLIENT_IP_HEADER` と両方設定すると、一致しないリクエストを 403 で拒否する（`/health`・`/internal/outbox/process` を除く。ADR-0034） | （未設定＝確認しない） |
| `RELAY_SECRET_PREVIOUS` | 値の差し替え中だけ設定する古い秘密値。`RELAY_SECRET` と同じく受け付ける | （未設定） |
| `RELAY_CLIENT_IP_HEADER` | 中継用 Worker が利用者 IP を入れるヘッダー名。確認に通ったリクエストに限り、IP レート制限とログの IP をこの値から読む。`RELAY_SECRET` と片方だけ設定すると起動しない | （未設定） |
| `CORS_ALLOW_ORIGINS` | CORS で許可するオリジン（カンマ区切り、`scheme://host[:port]` の形）。末尾スラッシュ・パス・`*` を含む値があると起動しない（ADR-0033） | `http://localhost:3000,http://localhost:8787` |
| `OUTBOX_WORKER_ENABLED` | `true` で、API プロセス内に通知メールの outbox を 30 秒間隔で処理する常駐ワーカーを起動する（ローカル開発用。本番は設定しない。ADR-0035） | `false` |
| `SCHEDULER_OIDC_AUDIENCE` | Cloud Scheduler の OIDC トークンの audience。`SCHEDULER_SERVICE_ACCOUNT_EMAIL` と両方設定すると `POST /internal/outbox/process` が有効になる（ADR-0035） | （未設定） |
| `SCHEDULER_SERVICE_ACCOUNT_EMAIL` | Cloud Scheduler のジョブが使うサービスアカウントのメールアドレス | （未設定） |
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
