# NatuEve_API

なちゅいべのバックエンド API サーバー。
生物イベントの集約・検索・管理を行う。

## Tech Stack
- Go + Gin
- PostgreSQL 17（Docker Compose）
- Supabase Auth（JWT 検証のみ、DB は使わない）
- golang-jwt + MicahParks/keyfunc（JWKS）

## Module Path
`github.com/GokujyouKaisennDonnburi/NatuEve_API`

## Project Structure
```
cmd/server/main.go    # エントリーポイント
internal/             # アプリケーションコード
docker-compose.yml    # PostgreSQL + API
.env.example          # 環境変数テンプレート
```

## Commands
```bash
go run cmd/server/main.go        # ローカル実行
go test ./...                    # テスト実行
docker compose up -d             # DB 起動
docker compose down              # DB 停止
```

### Make ターゲットを優先する
`Makefile` にターゲットがある作業は、素のコマンドを直接叩かず必ず `make <target>` を使う。
ツール版数の固定や生成オプションが Makefile に集約されており、直接実行するとバージョンずれで
生成物がブレ、CI（`make swag-check` 等）で落ちる。
- OpenAPI ドキュメント生成: `swag init` ではなく `make swag`
- 何か作業する前に `make help` で利用可能なターゲットを確認する

## Architecture Rules

### CRITICAL: 座標情報の保護
希少種・外来種の生息地座標はクライアントからの直書き込みを**禁止**する。
座標ぼかし（geofuzzing）と公開範囲制御は必ず API サーバー側で強制する。
クライアントが送信した raw 座標をそのまま DB に保存するコードを書いてはならない。

### ドメインロジックの集約
バリデーション・権限チェック・位置情報保護処理は API サーバーで行う。
クライアント側のバリデーションは UX 補助のみで、サーバー側が信頼の境界。

### 認証
- Supabase Auth の JWKS エンドポイントで JWT を検証する
- Supabase の DB・Storage 機能は使用しない
- 認証基盤は NatuPortal の複数プロダクトで共有

### 型共有
- OpenAPI 定義からGoの型を自動生成する
- 手書きの API 型定義は作らない

## Conventions
- エラーレスポンスは統一フォーマット（`{"error": {"code": "...", "message": "..."}}`)
- ハンドラは `internal/handler/`、ビジネスロジックは `internal/service/`
- DB アクセスは `internal/repository/`
- 環境変数は `.env` + godotenv、ハードコーディング禁止
- **`.env` は絶対に読まない**（Supabase 鍵・JWT シークレット等を含む）。変数名や形式を知りたい時は `.env.example` を参照する

<!-- 詳細ルールは .claude/rules/ に配置（Claude Code 2.0.64+ が自動ロード） -->
