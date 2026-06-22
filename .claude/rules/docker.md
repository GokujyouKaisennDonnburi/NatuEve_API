# Docker ルール

> 適用対象: `Dockerfile*`, `docker-compose*.yml`, `.dockerignore`。
> このファイルは CLAUDE.md から `@import` で常時ロードされる。

## 方針
- マルチステージビルドを使用する（builder → runtime）
- runtime ステージは `gcr.io/distroless/static-debian12` または alpine ベース
- `.env` ファイルは Docker イメージに含めない
- `docker-compose.yml` でヘルスチェックを定義する
- ボリュームでデータを永続化する（PostgreSQL）

## 将来の移行
現在は Xserver VPS + Docker Compose。1年後に AWS App Runner/ECS + RDS/Aurora へ移行予定。
移行コストを最小化するため、Docker イメージの可搬性を維持する。
