#!/usr/bin/env bash
# Cloud Run の API をデプロイする（.github/workflows/deploy.yml から呼ぶ。ADR-0040）。
# 使い方: deploy/cloud-run-deploy.sh <staging|production> <イメージ（ダイジェスト指定）>
#
# 環境変数（GitHub Environments の変数）:
#   必須: GCP_PROJECT_ID, CLOUD_RUN_SERVICE, GCP_RUNTIME_SERVICE_ACCOUNT
#   空なら Cloud Run に設定しない: SUPABASE_JWKS_URL, R2_ACCOUNT_ID, R2_BUCKET, MAIL_FROM,
#     SCHEDULER_OIDC_AUDIENCE, SCHEDULER_SERVICE_ACCOUNT_EMAIL
#   RELAY_SECRET_PREVIOUS_VERSION: 中継元の秘密値の差し替え中だけ、RELAY_SECRET の古いバージョン番号を入れる
#
# 環境変数は deploy/cloud-run/<環境>.yaml の値に上の変数を足したもので置き換え、秘密値は Secret Manager から渡す。
# Cloud Run に手で足した環境変数・シークレットは、このスクリプトの実行で消える。
set -euo pipefail

environment="${1:?環境（staging または production）を指定してください}"
image="${2:?イメージを指定してください}"
: "${GCP_PROJECT_ID:?GCP_PROJECT_ID が未設定です}"
: "${CLOUD_RUN_SERVICE:?CLOUD_RUN_SERVICE が未設定です}"
: "${GCP_RUNTIME_SERVICE_ACCOUNT:?GCP_RUNTIME_SERVICE_ACCOUNT が未設定です}"

base_env_file="deploy/cloud-run/${environment}.yaml"
if [ ! -f "$base_env_file" ]; then
  echo "環境の設定ファイルがありません: ${base_env_file}" >&2
  exit 1
fi

env_file="$(mktemp)"
trap 'rm -f "$env_file"' EXIT
cp "$base_env_file" "$env_file"
for name in SUPABASE_JWKS_URL R2_ACCOUNT_ID R2_BUCKET MAIL_FROM SCHEDULER_OIDC_AUDIENCE SCHEDULER_SERVICE_ACCOUNT_EMAIL; do
  value="${!name:-}"
  if [ -n "$value" ]; then
    printf '%s: "%s"\n' "$name" "$value" >>"$env_file"
  fi
done

secrets="DATABASE_URL=DATABASE_URL:latest"
secrets+=",RELAY_SECRET=RELAY_SECRET:latest"
secrets+=",R2_ACCESS_KEY_ID=R2_ACCESS_KEY_ID:latest"
secrets+=",R2_SECRET_ACCESS_KEY=R2_SECRET_ACCESS_KEY:latest"
secrets+=",RESEND_API_KEY=RESEND_API_KEY:latest"
if [ -n "${RELAY_SECRET_PREVIOUS_VERSION:-}" ]; then
  secrets+=",RELAY_SECRET_PREVIOUS=RELAY_SECRET:${RELAY_SECRET_PREVIOUS_VERSION}"
fi

# CPU・メモリ・最大インスタンス数は docs/Deployment.md の値（ADR-0037）。
gcloud run deploy "$CLOUD_RUN_SERVICE" \
  --project "$GCP_PROJECT_ID" \
  --region asia-northeast1 \
  --image "$image" \
  --service-account "$GCP_RUNTIME_SERVICE_ACCOUNT" \
  --allow-unauthenticated \
  --cpu 1 \
  --memory 512Mi \
  --max 1 \
  --env-vars-file "$env_file" \
  --set-secrets "$secrets" \
  --quiet
