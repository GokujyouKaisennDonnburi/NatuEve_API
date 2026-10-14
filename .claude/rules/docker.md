# Docker ルール

> 適用対象: `Dockerfile*`, `docker-compose*.yml`, `.dockerignore`。
> このファイルは `.claude/rules/` から常時自動ロードされる。

## 方針
- マルチステージビルドを使用する（builder → runtime）
- runtime ステージは `gcr.io/distroless/static-debian12` または alpine ベース
- `.env` ファイルは Docker イメージに含めない
- `docker-compose.yml` でヘルスチェックを定義する
- ボリュームでデータを永続化する（PostgreSQL）

## デプロイ先
本番は Cloud Run（ADR-0032、`docs/Deployment.md`）。`docker-compose.yml` は開発用。
- コンテナは環境変数 `PORT` で待ち受ける（Cloud Run が値を注入する）
- 特定の実行基盤に依存させず、Docker イメージの可搬性を維持する
