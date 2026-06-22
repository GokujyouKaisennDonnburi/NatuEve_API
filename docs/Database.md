# データベース / マイグレーション

PostgreSQL とマイグレーション（[goose](https://github.com/pressly/goose)）の運用をまとめる。
環境構築の全体像は [Tech.md](./Tech.md)、開発規約は [Development.md](./Development.md) を参照。

## 構成

- **DB**: PostgreSQL（開発は docker compose の `db` サービス、本番は外部のマネージド Postgres を想定）。
- **ドライバ**: `github.com/jackc/pgx/v5`（`pgx/stdlib` 経由で標準の `database/sql` に載せる）。
- **マイグレーション**: `github.com/pressly/goose/v3`。素の SQL ファイルで `up` / `down` を管理する。
- マイグレーション SQL は `db/db.go` が `//go:embed` で**バイナリに埋め込む**。これにより、起動時の自動適用（開発）と CLI（goose）からの適用が同じ定義を使う。

```
db/
  db.go                 接続(Open)とマイグレーション適用(Migrate)。migrations/ を embed
  migrations/           goose のマイグレーション SQL（コミット対象）
    20260622120000_create_users.sql
```

> アプリ層の DB アクセスは `internal/repository/` に置く（[Development.md](./Development.md) の責務分担を参照）。`db/` は接続とマイグレーション基盤のみ。

## 環境変数

| 変数 | 説明 | デフォルト |
|---|---|---|
| `DATABASE_URL` | Postgres 接続文字列。**未設定なら DB に接続せず起動する** | （未設定） |
| `DB_AUTO_MIGRATE` | `true` のとき起動時にマイグレーションを自動適用（開発用）。本番は `false` | `false` |

接続文字列の例:

```
postgres://app:app@localhost:5432/natuive?sslmode=disable
```

- **docker compose 利用時**は `docker-compose.yml` の `api` サービスで `DATABASE_URL`（`@db:5432`）と `DB_AUTO_MIGRATE=true` を自動設定するため、`.env` 側は未設定でよい。
- **ローカルで直接 Postgres に繋ぐ / `make migrate-*` を使う**場合は `.env` の `DATABASE_URL` を有効化する。
- 本番は `.env` を使わず実行環境の環境変数で注入する（機密は `.env` にコミットしない）。

## make コマンド

| コマンド | 用途 |
|---|---|
| `make setup` | 初期セットアップ（`.env` 作成・依存取得・CLI 一括導入。goose 含む） |
| `make migrate-install` | goose CLI を導入（バージョン固定 `GOOSE_VERSION`） |
| `make migrate-create name=create_xxx` | マイグレーション雛形を作成 |
| `make migrate-up` | マイグレーションを最新まで適用 |
| `make migrate-down` | マイグレーションを 1 つ戻す |
| `make migrate-status` | 適用状況を表示 |

> `migrate-*` は `DATABASE_URL` を使う。Makefile は `.env` を自動読み込みするので、`.env` に `DATABASE_URL` があればそのまま動く。CI など `.env` が無い環境ではシェルの環境変数が使われる。

## 開発の流れ

### docker compose（推奨）

`make up` で `db`（Postgres）が起動し、`api` は DB が healthy になってから起動する。
`DB_AUTO_MIGRATE=true` のため、**起動時に未適用のマイグレーションが自動で適用される**。

```bash
make up   # db 起動 → api 起動時に migrate up が走る
```

### ローカルで直接動かす

別途 Postgres を用意し、`.env` の `DATABASE_URL` を設定したうえで:

```bash
make migrate-up   # 手動で適用
make run          # サーバ起動
```

## マイグレーションの書き方

`make migrate-create name=create_xxx` で `db/migrations/<timestamp>_create_xxx.sql` が作られる。
1 ファイルに `Up` と `Down` を両方書く（ロールバックできるようにする）。

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE example (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE example;
-- +goose StatementEnd
```

- **マーカー**: `-- +goose Up` / `-- +goose Down` で方向を区切る。
- **`StatementBegin` / `StatementEnd`**: 関数定義など `;` を含む複数文や、複文を 1 まとまりとして実行したいときに囲む。単純な 1 文だけなら省略可。
- **順序**: ファイル名先頭の数値（タイムスタンプ）で適用順が決まる。適用済みバージョンは DB の `goose_db_version` テーブルで管理される。
- **適用済みファイルは編集しない**。変更は必ず新しいマイグレーションを追加して行う。
- マイグレーション SQL は `db/migrations/` にコミットする（embed されるため、ビルドにも必要）。

## 本番でのマイグレーション

本番は `DB_AUTO_MIGRATE=false`（起動時に勝手にスキーマが変わらないように）。
デプロイ手順の中で明示的に適用する。

```bash
DATABASE_URL=... make migrate-up
```

## CI での検証

`.github/workflows/ci.yml` の `migrate` ジョブが、空の Postgres に対して
`up` → `down` → `up` のラウンドトリップを実行し、マイグレーションが正しく適用・ロールバックできることを検証する。
