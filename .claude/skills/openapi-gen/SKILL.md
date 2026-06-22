---
name: openapi-gen
description: >
  OpenAPI 定義から Go/TypeScript/Kotlin の型コードを生成する。
  「型生成」「OpenAPI」「スキーマ同期」と言われたときに使用する。
---

# OpenAPI 型生成

## 前提
- OpenAPI 定義ファイルは `api/openapi.yaml` に配置
- Go / TypeScript / Kotlin の3言語に対して型を生成する

## 手順
1. `api/openapi.yaml` を読み込む
2. 以下のコマンドで各言語の型を生成:
   - Go: `oapi-codegen` を使用
   - TypeScript: `openapi-typescript` を使用
   - Kotlin: `openapi-generator-cli` を使用
3. 生成されたコードは手動編集しない
4. OpenAPI 定義を変更したら、必ず全言語の型を再生成する

## 注意
- 手書きの API 型定義を作成しないこと
- 生成コードと手書きコードの混在を避ける
