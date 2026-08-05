# 開発ガイド

実装者向けの規約と注意点をまとめる。環境構築・起動手順は [Tech.md](./Tech.md) を参照。

## コマンド（Makefile）

`make` または `make help` でタスク一覧が出る。

docker コマンドは基本使わない。make経由で使用する。

| コマンド | 用途 |
|---|---|
| `make setup` | 初期セットアップ（`.env` 作成・依存取得・CLI 一括導入） |
| `make up` | docker compose up 開発用コンテナの起動 |
| `make down` | docker compose down 開発用コンテナの停止 |
| `make build` | ビルド確認 |
| `make test` | テスト実行 |
| `make tidy` / `make fmt` / `make vet` | 依存整理 / 整形 / 静的解析 |
| `make lint-install` | golangci-lint を導入（バージョン固定） |
| `make lint` | golangci-lint を実行（`.golangci.yml`） |
| `make swag-install` | swag CLI を導入（バージョン固定） |
| `make swag` | OpenAPI ドキュメント（`api/`）を再生成 |
| `make swag-check` | `api/` が最新か検証（CI用。差分があれば失敗） |
| `make migrate-create name=…` / `migrate-up` / `migrate-down` / `migrate-status` | マイグレーション操作（`make up` で起動中の api コンテナ内で実行。[Database.md](./Database.md)） |
| `make run` | 基本使わない。ローカルでサーバ起動（`go run ./cmd/api`） |

> **初回は `make setup`** で `.env` 作成・依存取得・各 CLI（swag / golangci-lint）の導入をまとめて行える。
> DB・マイグレーションの詳細は [Database.md](./Database.md) を参照。

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
api/                   swag 生成物（docs.go / swagger.json|yaml）。手編集禁止
docs/                  人間が書く Markdown（本ガイド・Tech.md など）
```

> **生成物と手書きドキュメントは分ける**: `api/` は `make swag` が作る生成物（触らない）、`docs/` は人が書く Markdown。

**依存方向は `handler → service → repository` の一方向**。逆流させない。
`repository` は interface で定義し、`service` のテスト時にモックへ差し替えられるようにする。

> `service` / `repository` は枠（doc.go）のみ。最初のデータ機能を追加するときに実装する。
> `model` には DTO（例: `HealthResponse`）を置く。空のまま増やさず、必要になった層から埋めること。

**レスポンスは `gin.H` ではなく `model` の型を返す**。型を介すことで Swagger のスキーマが正確になり（`@Success ... {object} model.Xxx`）、フィールドの整合も取れる。

## 命名規則

Go の標準スタイル（[Effective Go](https://go.dev/doc/effective_go) / [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)）に従う。`make fmt`（gofmt）と `make vet` を必ず通す。

- **パッケージ名**: 小文字 1 語、アンダースコア・複数形にしない（`handler`, `config`。`handlers` や `user_service` は避ける）。ディレクトリ名 = パッケージ名にする。
- **公開/非公開**: 外部公開する識別子は大文字始まり（`NewRouter`）、パッケージ内部のみは小文字始まり（`registerRoutes`）。**公開は必要なものだけ**にする。
- **イニシャリズム**: 頭字語は大小をそろえる（`ID` / `URL` / `HTTP` / `API`。`Id` `Url` は不可。例: `userID`, `JWKSURL`）。
- **コンストラクタ**: `NewXxx`（例: `NewHealthHandler`）。
- **interface 名**: 単一メソッドは「メソッド名 + er」（`Reader`）。リポジトリは役割で命名（`UserRepository`）。
- **変数名**: スコープが狭いほど短く（ループの `i`、レシーバは 1〜2 文字）。スコープが広いほど説明的に。
- **エラー**: センチネルは `ErrXxx`、型は `XxxError`。`err` の使い回しでよい。
- **ファイル名**: 小文字スネーク不可、ロワーキャメルも使わない。機能単位で小文字（`health.go`, `logger.go`）。テストは `xxx_test.go`。
- **環境変数**: 大文字スネーク（`TRUSTED_PROXIES`, `SUPABASE_JWKS_URL`）。

## レスポンス DTO の欠損表現

「値が無い」ことをレスポンスでどう表すかは `null` / キー省略（`omitempty`）/ ゼロ値の 3 択になる。
毎回議論しないよう基準を決めておく（判断の経緯は [ADR-0022](./adr/0022-event-member-profile-summary.md)）。

**原則は `null`（ポインタ型）**。欠損そのものがクライアントにとって意味を持つなら、ポインタ + `null` で明示する。
「キーが無い」は「この参加者にプロフィールが無い」とも「この API バージョンにその項目がまだ無い」とも読めて
多義的だが、`null` は「調べた上で無い」と言い切れる。

**`omitempty` は次の 3 つを全て満たすときだけ使う。**

1. 欠損が情報として重要でない（該当しないことが自然に読める）
2. 要素数が多く、省略で実際にペイロードが軽くなる
3. 同じレスポンス内の他フィールドが、同じ事実を `null` で表していない

満たす例は `EventSummary.Tags`（タグ無しイベントで `"tags":[]` を並べるより軽い / ADR-0015）と
`EventSummary.CancelledAt`（キャンセル済みは少数で、該当しないイベントには項目自体が無意味）。
満たさない例は `EventMemberResponse.Profile`（欠損＝匿名参加が主催者にとって意味のある情報で、
隣の `profileId` が同じ事実を `null` で表している）。

**nullable なフィールドには `extensions:"x-nullable"` を付ける**。swag が生成するのは Swagger 2.0 で、
Go のポインタから nullable を自動推論しない。タグが無いと生成される型が非 null になり、クライアント側の
null 処理漏れがコンパイル時に検出できなくなる。

```go
// ProfileID は参加者のプロフィールUUID。匿名参加の場合は null。
ProfileID *uuid.UUID `json:"profileId" extensions:"x-nullable"`
// Profile は参加者のプロフィールサマリー。匿名参加の場合は null。
Profile *ProfileSummary `json:"profile" extensions:"x-nullable"`
```

### 落とし穴

- **`omitempty` は非ポインタの構造体には効かない**。ゼロ値でもキーは必ず出る。効くのは nil ポインタ・
  空スライス・空マップ・空文字・ゼロ数値・`false`。構造体を省略したいならポインタが必須になる。
- **意味のあるゼロ値が消える**。`false` や `0` が有効な値のフィールドに付けると、正しい値なのにキーごと消える。
- **`omitempty` は OpenAPI の出力を変えない**。付けても付けなくても `make swag` の生成物は同一
  （#162 で実測）。クライアントの生成型も変わらないため、効果は JSON のバイト数だけ。
- **配列に `null` を返さない**。`omitempty` を付けないなら `make([]T, 0, n)` で空配列にする
  （nil スライスは `null` になる）。

## godoc / ドキュメントコメント

公開要素には godoc コメントを書く。`go doc ./internal/...` や pkg.go.dev と同じ形式で読める。

- **対象**: 公開（大文字始まり）の型・関数・メソッド・パッケージには原則コメントを付ける。非公開でも意図が非自明なら書く。
- **書き出しは名前から**: `// NewRouter は ...` のように **要素名で始める**（godoc がそのまま見出しにする）。
- **パッケージコメント**: 各パッケージに 1 つ。`doc.go` か主要ファイルの `package` 直前に `// Package xxx は ...` を書く（本リポジトリは `doc.go` 方式）。
- **Swagger アノテーション**: ハンドラの godoc コメント内に `@Summary` 等を併記する（[Swagger](#api-ドキュメントswagger) 参照）。コメントとアノテーションは同じブロックにまとめる。
- **整形**: コメントは平文＋空行で段落、コードは 1 タブインデント。`gofmt` がコメントも整える。

例:
```go
// HealthHandler はヘルスチェック系のエンドポイントを担当する。
type HealthHandler struct{}

// Check はサーバーの稼働状態を返す。
//
//	@Summary	ヘルスチェック
//	@Tags		system
//	@Success	200	{object}	model.HealthResponse
//	@Router		/health [get]
func (h *HealthHandler) Check(c *gin.Context) { ... }
```

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

ハンドラのコメントに書いた `@Summary` 等のアノテーションから OpenAPI を生成し、Swagger UI で見られる。
UI: サーバ起動後に `http://localhost:8085/swagger/index.html`。

### swaggerUIでの検証方法

#### googleにリダイレクトするURL

https://[.env参照].supabase.co/auth/v1/authorize?provider=google&redirect_to=http://localhost:3003

#### swaggerのAuthorizeに入れる値

Bearer + access_token= ~ &expires_atまでの値

入れるとログイン状態になる

#### 実行

ParametersのTry it out　を実行する

すると試せる

#### 生成の仕組み（2 フェーズ）

```
[1. 生成] make swag (swag init)
   Go のコメント ──静的解析(AST)──> api/docs.go・swagger.json・swagger.yaml

[2. 配信] サーバ起動時
   api を blank import ──init() で登録──> gin-swagger が /swagger で配信
```

1. **生成**: `make swag`（= `swag init`）が**サーバを起動せず、ソースのコメントを解析**して `api/` を作る。
   - `cmd/api/main.go` の `@title` `@version` `@BasePath` … 全体情報
   - 各ハンドラの `@Summary` `@Tags` `@Success` `@Router` … 各エンドポイント
   - `--parseInternal` / `--parseDependency` で `internal/` と依存先の型も解析する。
2. **配信**: `internal/server/router.go` が `_ "…/api"` を **blank import** → `docs.go` の `init()` が spec を登録 → `gin-swagger` が `/swagger/*` で UI と `doc.json` を返す。

> **重要: swag は Gin のルート（`r.GET(...)`）を見ない。** `@Router /health [get]` という**コメントの文字列だけ**で spec を作る。
> そのため実ルートと `@Router` がズレるとドキュメントと実装が食い違うので、両方を一致させること。

### 自動生成ではない（手動 → コミット）

コメントを変えただけでは反映されない。**自分で `make swag` を実行して `api/` を作り直し、コミットする。**

```bash
# ハンドラの @Summary 等を編集したら
make swag
git add api/ && git commit -m "docs: swagger 再生成 #1"
```

- `api/` はリポジトリにコミットする運用（ビルドや起動時には生成せず、コミット済みの `api/` を読む）。
- swag のバージョンは `Makefile` の `SWAG_VERSION` で固定（go.mod の `swaggo/swag` と一致させ、版違いの差分を防ぐ）。

### 最新かは CI でチェックされる

`make swag-check` が **再生成して差分が出たら失敗**する。再生成漏れを自動で検知する仕組み。

```make
swag-check: swag                 # ① api/ を再生成
	git diff --exit-code ./api   # ② 差分があれば exit 1（＝失敗）
```

- 一致していれば差分ゼロで成功。再生成し忘れていれば差分が出て失敗する。
- CI（[後述](#ci-github-actions)）の PR ごとに走る。**検知するだけで自動修正はしない**ので、落ちたら上記の手順で `make swag` → コミットする。

## CI（GitHub Actions）

`main` への push と全 PR で `.github/workflows/ci.yml` が動く。実行内容:

`make swag-check`（docs 更新漏れ） → `make vet` → `make lint` → `make build` → `make test`

ローカルでも同じ `make` ターゲットで再現できる。push 前に `make swag-check vet lint build test` を回すと CI を落としにくい。

> `make lint` には golangci-lint が必要。初回のみ `make lint-install`（バージョン固定）で導入する。
> 設定は `.golangci.yml`（標準セット + bodyclose/errorlint/gocritic/gosec/misspell/revive、生成物の `api/` は対象外）。

## 実装時の注意点

- **機密情報を `.env` にコミットしない**。`.env` は `.gitignore` 済み、共有はキー名のみ `.env.example` で行う。本番は実行環境の環境変数 / シークレットマネージャで注入する（[.env.example](../.env.example) 参照）。
- **`SetTrustedProxies`**: 未設定（nil）はどのプロキシも信頼しない。プロキシ越しに置く場合のみ `TRUSTED_PROXIES` に CIDR を設定する。
- **認証**: Supabase が発行する JWT を JWKS で検証する想定。実装時は `internal/middleware/` に追加する。
