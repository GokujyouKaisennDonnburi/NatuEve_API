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
- ハンドラの godoc コメントに `@Summary` `@Success {object} model.Xxx` 等を併記する
- `make swag` が Go のソースを静的解析して OpenAPI（`api/`）を生成する
- `internal/model/` の API 型は手書きで定義し、swag 出力と乖離しないよう `@Router` / `@Success` のコメントで参照する
- フロント等他言語の型を OpenAPI から生成したい場合は、生成元を `api/swagger.yaml`（make swag の成果物）にする

## ドキュメント（ADR）

### 設計判断は ADR に書き、コードコメントには事実だけ残す
「なぜこの方式にしたか」「どの代替案をなぜ退けたか」は `docs/adr/` の ADR に書く。
コードコメント・godoc に書いてよいのは事実と契約だけで、判断の理由は書かない。

- コメントに書いてよいもの: 何をしているか / 呼び出し側が守るべき前提 / 依存している制約・既知の穴 /
  順序や境界条件 / `（ADR-00XX）` という参照
- コメントに書かないもの: その方式を選んだ理由、代替案との比較、トレードオフの評価
- 理由を書きたくなったら ADR へ移し、コメントには `（ADR-00XX）` だけ残す。
  同じ理由を両方に書かない（片方だけ古くなって矛盾するため）
- 新しい判断を伴う実装は、実装前に ADR を起こす（`docs/adr/template.md` をコピーし、
  `docs/adr/README.md` の一覧表に行を追加する）
- ADR を立てるほどでない小さな判断は、関連する既存 ADR に1行足すか、理由を書かず事実だけ残す
- 例外: swag の `@Description` は API 利用者向けのドキュメントなので、
  利用者が知るべき仕様（返さない項目・エラー条件など）はそのまま書いてよい

## Conventions
- エラーレスポンスは統一フォーマット（`{"error": {"code": "...", "message": "..."}}`)
- ハンドラは `internal/handler/`、ビジネスロジックは `internal/service/`
- DB アクセスは `internal/repository/`
- 環境変数は `.env` + godotenv、ハードコーディング禁止
- **`.env` は絶対に読まない**（Supabase 鍵・JWT シークレット等を含む）。変数名や形式を知りたい時は `.env.example` を参照する

<!-- 詳細ルールは .claude/rules/ に配置（Claude Code 2.0.64+ が自動ロード） -->
