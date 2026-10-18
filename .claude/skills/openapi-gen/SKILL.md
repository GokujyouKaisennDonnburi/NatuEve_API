---
name: openapi-gen
description: >
  Go の API 型とハンドラの swag 注釈から OpenAPI（api/）を生成し、実装と同期させる。
  「型生成」「OpenAPI」「スキーマ同期」「swag」と言われたときに使用する。
---

# OpenAPI 生成（swag）

## 前提
- API 型は `internal/model/` に手書きで定義する（生成物ではない）
- OpenAPI は `make swag` が `cmd/api/main.go` の全体注釈とハンドラの godoc 注釈を静的解析して生成する
- 生成物は `api/`（`docs.go` / `swagger.json` / `swagger.yaml`）。コミット対象で、手編集しない
- 生成される仕様は Swagger 2.0（`swagger: "2.0"`）

## 手順
1. `internal/model/` の API 型を追加・変更する
2. ハンドラの godoc に swag 注釈を書く・更新する
   （`@Summary` `@Description` `@Tags` `@Produce` `@Success {object} model.Xxx` `@Failure` `@Router` 等。例: `internal/handler/tag.go`）
3. `make swag` で `api/` を再生成する（`swag init` を直接実行しない）
4. `api/` の差分を確認し、型・ハンドラの変更と同じコミットに含める

## 注意
- swag CLI が未導入なら `make swag-install`（バージョン固定）
- CI（`.github/workflows/ci.yml`）で `make swag-check` が走り、再生成した `api/` がチェックアウトした内容と差分を持つと失敗する
- `make swag-check` は再生成後に `git diff --exit-code ./api` で作業ツリーとインデックスを比べる。ローカルでは `git add api/` 後だと HEAD と差があっても通り、未追跡の新規ファイルも検出しない
- フロント等他言語の型を生成する場合は、生成元を `api/swagger.yaml` にする
