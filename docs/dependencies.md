# 使用ライブラリ

実際のバージョンは `go.mod` / `go.sum` が正(single source of truth)。
このドキュメントは「なぜ入れているか」という用途を残すためのもの。

## 導入済み（go.mod に登録済み）

| ライブラリ | 用途 |
|---|---|
| [gin-gonic/gin](https://github.com/gin-gonic/gin) | HTTP ルーティング / ミドルウェア（Web フレームワーク） |
| [joho/godotenv](https://github.com/joho/godotenv) | 開発時の `.env` 読み込み |
| [jackc/pgx/v5](https://github.com/jackc/pgx) | PostgreSQL ドライバ（`pgx/stdlib` 経由で `database/sql` に接続） |
| [pressly/goose/v3](https://github.com/pressly/goose) | DB マイグレーション（SQL を embed し起動時/CLI で適用。[Database.md](./Database.md)） |

## 導入予定（コード未使用のため go mod tidy で go.mod から外れている）

以下は認証処理を実装する際に import すれば自動的に `go.mod` の直接依存へ戻る。
ダウンロード自体は済んでいるためオフラインでも `go get` し直せる。

| ライブラリ | 用途 |
|---|---|
| [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) | JWT のパース・検証 |
| [MicahParks/keyfunc/v3](https://github.com/MicahParks/keyfunc) | Supabase の JWKS を取得し JWT 検証に利用 |
