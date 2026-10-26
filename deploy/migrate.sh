#!/usr/bin/env bash
# Secret Manager の DATABASE_URL を読み、マイグレーションを適用する（.github/workflows/deploy.yml から呼ぶ。ADR-0040）。
# 環境変数: GCP_PROJECT_ID（必須）
set -euo pipefail

: "${GCP_PROJECT_ID:?GCP_PROJECT_ID が未設定です}"

url="$(gcloud secrets versions access latest --secret=DATABASE_URL --project="$GCP_PROJECT_ID")"
# 接続文字列とパスワードをログに出さない。
echo "::add-mask::${url}"
password="$(printf '%s' "$url" | sed -nE 's#^[^:]+://[^:@/]+:([^@]+)@.*#\1#p')"
if [ -n "$password" ]; then
  echo "::add-mask::${password}"
fi

DATABASE_URL="$url" make migrate-up GOOSE_EXEC='sh -c'
