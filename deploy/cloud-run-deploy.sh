#!/usr/bin/env bash
# Cloud Run の API をデプロイする（.github/workflows/deploy.yml から呼ぶ。ADR-0040）。
# 使い方: deploy/cloud-run-deploy.sh <staging|production> <イメージ（ダイジェスト指定）>
#
# 環境変数（GitHub Environments の変数。CLOUD_RUN_SERVICE と SCHEDULER_OIDC_AUDIENCE はシークレット）:
#   必須: REGION（ワークフローの env）, GCP_PROJECT_ID, CLOUD_RUN_SERVICE, GCP_RUNTIME_SERVICE_ACCOUNT, SUPABASE_JWKS_URL, R2_ACCOUNT_ID, R2_BUCKET, MAIL_FROM
#   空なら Cloud Run に設定しない: SCHEDULER_OIDC_AUDIENCE, SCHEDULER_SERVICE_ACCOUNT_EMAIL
#   値に " と \ は含められない
#   RELAY_SECRET_PREVIOUS_VERSION: 中継元の秘密値の差し替え中だけ、RELAY_SECRET の古いバージョン番号（数字）を入れる
#
# 環境変数は deploy/cloud-run/<環境>.yaml の値に上の変数を足したもので置き換え、秘密値は Secret Manager から渡す。
# Cloud Run に手で足した環境変数・シークレットは、このスクリプトの実行で消える。
set -euo pipefail

environment="${1:?環境（staging または production）を指定してください}"
image="${2:?イメージを指定してください}"
for name in REGION GCP_PROJECT_ID CLOUD_RUN_SERVICE GCP_RUNTIME_SERVICE_ACCOUNT SUPABASE_JWKS_URL R2_ACCOUNT_ID R2_BUCKET MAIL_FROM; do
  if [ -z "${!name:-}" ]; then
    echo "${name} が未設定です" >&2
    exit 1
  fi
done

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
  if [ -z "$value" ]; then
    continue
  fi
  case "$value" in
    *'"'* | *'\'*)
      echo "${name} の値に \" または \\ を含められません" >&2
      exit 1
      ;;
  esac
  printf '%s: "%s"\n' "$name" "$value" >>"$env_file"
done

secrets="DATABASE_URL=DATABASE_URL:latest"
secrets+=",RELAY_SECRET=RELAY_SECRET:latest"
secrets+=",R2_ACCESS_KEY_ID=R2_ACCESS_KEY_ID:latest"
secrets+=",R2_SECRET_ACCESS_KEY=R2_SECRET_ACCESS_KEY:latest"
secrets+=",RESEND_API_KEY=RESEND_API_KEY:latest"
if [ -n "${RELAY_SECRET_PREVIOUS_VERSION:-}" ]; then
  if [[ ! "$RELAY_SECRET_PREVIOUS_VERSION" =~ ^[1-9][0-9]*$ ]]; then
    echo "RELAY_SECRET_PREVIOUS_VERSION はバージョン番号（数字）にしてください" >&2
    exit 1
  fi
  secrets+=",RELAY_SECRET_PREVIOUS=RELAY_SECRET:${RELAY_SECRET_PREVIOUS_VERSION}"
fi

# CPU・メモリ・最大インスタンス数は docs/Deployment.md の値（ADR-0037）。
# 出力の run.app の URL は置き換えてからログに出す（ADR-0040 決定事項 9）。
gcloud run deploy "$CLOUD_RUN_SERVICE" \
  --project "$GCP_PROJECT_ID" \
  --region "$REGION" \
  --image "$image" \
  --service-account "$GCP_RUNTIME_SERVICE_ACCOUNT" \
  --allow-unauthenticated \
  --cpu 1 \
  --memory 512Mi \
  --max 1 \
  --env-vars-file "$env_file" \
  --set-secrets "$secrets" \
  --quiet 2>&1 | sed -E 's#https://[A-Za-z0-9.-]+\.run\.app#https://<run.app>#g'
