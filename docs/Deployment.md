# デプロイ構成（バックエンド）

NatuEve_API の本番デプロイ構成をまとめる。
構成を選んだ理由と退けた代替案は [ADR-0032](./adr/0032-production-deployment-cloud-run-supabase.md) を参照。
API の独自ドメインと中継の方式は [ADR-0034](./adr/0034-api-custom-domain-workers-relay.md) を参照。
最大インスタンス数と DB の接続数の上限は [ADR-0037](./adr/0037-cloud-run-max-instances.md) を参照。
R2 のバケットの分け方は [ADR-0039](./adr/0039-r2-production-bucket-split.md) を参照。
デプロイの自動化と GCP のプロジェクトの分け方は [ADR-0040](./adr/0040-github-actions-deploy-separate-gcp-projects.md) を参照。
料金・仕様は 2026-10 時点で各公式ページを確認した値（出典は末尾）。

## 使用するサービス

| サービス | 用途 | 状態 |
|---|---|---|
| **Google Cloud Run**（`asia-northeast1`） | API コンテナの実行 | 確定 |
| **Google Cloud Scheduler** | outbox 処理エンドポイントの定期呼び出し（1 分間隔） | 確定 |
| **Supabase**（`ap-northeast-1`） | PostgreSQL 17（DB） | 確定 |
| **Supabase Auth** | 認証（JWT 発行・JWKS による検証） | 確定（既存） |
| **Cloudflare Workers** | API の独自ドメインの中継（[ADR-0034](./adr/0034-api-custom-domain-workers-relay.md)） | 確定（`workers/api-relay/`。独自ドメイン経由の検証は検証環境が #254、本番が #209） |
| Cloudflare R2 | 画像・PDF の保存と配信 | 既存 |
| Resend | トランザクションメールの送信（[ADR-0001](./adr/0001-resend-transactional-email.md)） | 既存 |
| **GitHub Actions** | 検証環境・本番へのデプロイ（承認は GitHub Environments。[デプロイ（CI/CD）](#デプロイcicd)） | 確定（ADR-0040） |
| Google Artifact Registry | コンテナイメージの保管（検証用・本番用のプロジェクトにそれぞれ置く） | 確定（ADR-0040） |
| Google Secret Manager | 秘密情報の注入（検証用・本番用のプロジェクトにそれぞれ置く） | 確定（ADR-0040） |

## 構成図

```
[Web フロント（SSR を含む） / モバイルアプリ]
   │
   ├─ ログイン ─────────────→ Supabase Auth（JWT 発行）
   │
   └─ API 呼び出し（JWT 付き）─→ Cloudflare Workers: 中継（natueve-api.natuportal.org）
                                   │ 秘密ヘッダーと利用者 IP を付けて run.app の URL へ転送
                                   ▼
                                 Cloud Run: NatuEve_API ←── Cloud Scheduler（1 分ごと、outbox 処理）
                                   │
                                   ├─→ Supabase PostgreSQL（DB）
                                   ├─→ Supabase Auth の JWKS（JWT 検証）
                                   ├─→ Cloudflare R2（presign・配信 URL の生成）
                                   └─→ Resend（メール送信）
```

## 各サービスの設定

### Cloud Run

| 項目 | 値 |
|---|---|
| リージョン | `asia-northeast1`（東京） |
| 課金方式 | リクエスト課金（CPU はリクエスト処理中のみ割り当て） |
| 最小インスタンス数 | 0 |
| 最大インスタンス数 | 1（サービス単位。CI がデプロイのたびに指定する。`deploy/cloud-run-deploy.sh`。ADR-0037） |
| 同時実行数 | 既定値（80）のまま |
| CPU / メモリ | 1 vCPU / 512MiB（初期値） |
| 待ち受けポート | 環境変数 `PORT`（Cloud Run が注入する値をそのまま使う） |
| ヘルスチェック | `GET /health`（秘密ヘッダーの確認から外している。ADR-0034） |
| イメージ | リポジトリ直下の `Dockerfile`（distroless・`./cmd/api`） |
| 認証 | なし（`allUsers` に invoker を付ける）。中継元は API が秘密ヘッダーで確かめる（ADR-0034） |
| run.app の URL | Worker の転送先としてだけ使い、クライアントには公開しない |

- リクエスト課金では、レスポンスを返した後の goroutine に CPU が割り当てられない。
  バックグラウンド処理を API プロセス内に置かない。
- 1 vCPU 未満にする場合は、同時実行数 1・第 1 世代実行環境が条件になる。
- 停止時は SIGTERM を受けてから 10 秒の猶予がある。
- 最大インスタンス数は、リビジョン単位では設定しない（リビジョンごとの上限で、デプロイ中は旧・新の合計が上限を超えうる）。
- **デプロイ後に確認する（未確認）**: サービス単位の上限が、デプロイ中の旧・新のリビジョンをどう数えるかは、公式ドキュメントで確認できていない。
  新しいリビジョンへの切り替え中に 429 が出ないかを、Cloud Run のログで確かめる。
- 通常の運用でも、最大インスタンス数を短時間超えることがある（公式: 週に数回）。その間は IP レート制限が最大 2 倍まで緩む。
- 最大インスタンス数に達すると、リクエストは「10 秒」と「インスタンスの平均起動時間の 3.5 倍」の長い方まで待ち、空かなければ 429 になる。
  正規の利用で 429 が出る場合は、最大インスタンス数を見直す（ADR-0037 の決定事項 5）。

### Cloud Scheduler

| 項目 | 値 |
|---|---|
| 呼び出し間隔 | 1 分 |
| 呼び出し先 | `POST /internal/outbox/process`（outbox を 1 回処理して `{"processed": n}` を返す） |
| 認証 | OIDC トークン。ジョブにサービスアカウントと audience を設定する（ADR-0035） |
| audience | 明示して指定し、Cloud Run の `SCHEDULER_OIDC_AUDIENCE` と同じ値にする |
| リトライ回数 | 明示して指定する（API の既定は 0 回。gcloud・Console の既定は未確認） |
| 試行の期限（attempt deadline） | 既定の 3 分のままでよい（1 回の呼び出しは、開始から 50 秒を過ぎると新しい行を確保しない） |

- 無料枠は請求アカウントあたり 3 ジョブ。ジョブを一時停止しても課金対象として数えられる。
- audience を省略すると、クエリを含むターゲット URL 全体（`https://<host>/internal/outbox/process`）が audience になる（公式ドキュメント）。
  `SCHEDULER_OIDC_AUDIENCE` と一致しないと、全呼び出しが 401 になる。
- ジョブを作る人には、ジョブのサービスアカウントに対する `roles/iam.serviceAccountUser` が必要。
  Cloud Scheduler のサービスエージェントに `roles/cloudscheduler.serviceAgent` が付いていること（2019-03-19 より前に API を有効化した場合は手動で付与する）。
- Scheduler の実行中は、そのジョブの次の実行はスキップされる。まれに同じジョブが重複して実行されることがあるが、行ごとの排他で、同じ行を同時に送ることはない。
- Scheduler が付けるトークンで、API の `iss`・`email`・`email_verified` の確認が通る（検証環境の手動実行で 200。#254）。
  ジョブを作ったら手動実行し、ログで 200 になることを確かめる（[初期設定](#初期設定1-回だけ)の手順 10）。
  401 なら audience、403 ならサービスアカウントのメールアドレスを疑う。

### Cloudflare Workers（API の中継）

| 項目 | 値 |
|---|---|
| プラン | Workers Paid（フロントと共有。NatuEve_Web_Frontend ADR-0001） |
| Worker 名 | `natueve-api-relay`（検証環境: `natueve-api-relay-stg`） |
| 割り当て | Custom Domain `natueve-api.natuportal.org`（検証環境: `natueve-api-stg.natuportal.org`） |
| 転送先 | Cloud Run の run.app の URL（固定。secret `ORIGIN_URL`） |
| 秘密ヘッダー | `X-Relay-Auth`（値は secret `RELAY_SECRET`。API の `RELAY_SECRET` と同じ値） |
| 利用者 IP のヘッダー | `X-NatuEve-Client-IP`（`wrangler.jsonc` の `CLIENT_IP_HEADER`。API の `RELAY_CLIENT_IP_HEADER` と同じ値） |
| `workers.dev`・Preview URL | 無効（`wrangler.jsonc` の `workers_dev: false`・`preview_urls: false`） |
| キャッシュ | 使わない（`fetch()` は `cache: "no-store"`。API のホスト名にキャッシュルールを付けない） |
| Pseudo IPv4（ゾーンの設定） | Off（既定） |
| コードの置き場所 | `workers/api-relay/` |

- 含まれる量はフロントと共有で、月 1,000 万リクエスト・CPU 3,000 万ミリ秒。超過は 100 万リクエストあたり $0.30、CPU 100 万ミリ秒あたり $0.02。
  `fetch()` の待ち時間は CPU 時間に数えられない。
- 転送時に付けるヘッダーと API 側の確認は ADR-0034 の決定事項 3・4。実装上の注意は ADR-0034 の「トレードオフ・制約」。
- 受けたリクエストに `CF-Connecting-IP` が無い場合、Worker は利用者 IP のヘッダーを付けずに転送する。その場合 API は接続元の IP で数えるため、
  該当するリクエストは IP レート制限を共有する（利用者 IP の検証は #215）。
- `ORIGIN_URL` と `RELAY_SECRET` は `wrangler.jsonc` の `secrets.required` に宣言しており、未設定だと `wrangler deploy` が失敗する。
  run.app の URL と秘密値はリポジトリに書かない（ADR-0034）。
- デプロイは CI が API の後に行う（[デプロイ（CI/CD）](#デプロイcicd)）。検証環境は `wrangler.jsonc` の `env.staging`。
- secret は環境ごとに 1 回だけ手で入れる。本番は `--env=""`（最上位の設定）を指定する。指定しないと、環境が複数あるため wrangler が警告を出す。

```bash
cd workers/api-relay
bun install --frozen-lockfile
bunx wrangler secret put ORIGIN_URL --env staging     # https://<サービス名>-<プロジェクト番号>.asia-northeast1.run.app
bunx wrangler secret put RELAY_SECRET --env staging   # その環境の API の RELAY_SECRET と同じ値
bunx wrangler secret put ORIGIN_URL --env=""          # 本番
bunx wrangler secret put RELAY_SECRET --env=""
```

- Worker が無い状態で `wrangler secret put` を実行すると、Worker を作るか聞かれ、`Y` で Worker が作られて secret が入る。
- `RELAY_SECRET` が API と一致しないと、独自ドメイン経由の `/health` 以外が 403 になる。値を画面に出さずに、両方の `sha256sum` の結果を比べて確かめる（`openssl rand -hex 32` で作った値なので、ハッシュから元の値は分からない）。

```bash
gcloud secrets versions access latest --secret=RELAY_SECRET --project <プロジェクト ID> | sha256sum   # API 側（Secret Manager）
read -rs -p "RELAY_SECRET: " v; echo; printf '%s' "$v" | sha256sum; unset v                      # Worker に入れる値
```

### Supabase

| 項目 | 値 |
|---|---|
| リージョン | `ap-northeast-1`（東京） |
| プラン | Free（本番公開後も Pro にしない。[ADR-0036](./adr/0036-github-flow-staging-approval.md)） |
| PostgreSQL | 17.11（2026-10-26 確認） |
| 接続方式 | Session pooler（ポート 5432、IPv4）。接続文字列の末尾に `?sslmode=require` を付ける |
| Data API（PostgREST） | 無効（ダッシュボードの「Enable Data API」をオフ。#214） |
| Storage | 使わない（ファイルは Cloudflare R2） |

- Free プランの制限: DB 500MB、自動バックアップなし、1 週間アクティビティがないと一時停止、アクティブなプロジェクトは 2 つまで
  （組織ごとではなくユーザーごとに数え、Owner / Administrator になっている全組織を合算する）。
- Pro プラン（$25/月〜）: 日次バックアップ（7 日保持）。コンピュート Micro（1GB RAM）1 つ分のクレジット（$10/月）を含む。
- Free のコンピュートは Nano。DB の最大接続数 60、プーラーの最大クライアント数 200。Auth・Storage・PostgREST なども同じ DB の接続数を使う。
- プールの大きさ（Pool Size）は「ユーザー・DB・モード」の組ごとの、プーラーが DB へ張る接続の上限。本番は 15（Nano の既定値。ダッシュボードで確認）。
  セッションモードでは、空きがないとクライアントは最大 1 分待たされ、空かなければエラーになる。
- `(最大インスタンス数 + 1) × DB_MAX_OPEN_CONNS + 2 ≦ Pool Size` を満たすこと（ADR-0037 の決定事項 4）。本番は `(1 + 1) × 6 + 2 = 14 ≦ 15`。
  Pool Size を変えたときと、検証環境のプロジェクトでも確かめる。満たさなければ `DB_MAX_OPEN_CONNS` を下げる。
- 既定では `public` スキーマに作ったテーブルに、API 用ロールの SELECT／INSERT／UPDATE／DELETE 権限が付く。
  RLS のないテーブルは、権限を持つロールなら誰でも読み書きできる。Data API が有効だと、公開されている anon キーで REST から読み書きできる。
  検証用 DB のプロジェクトでも Data API を無効にする。

### Cloudflare R2

| 項目 | 本番 | ローカル開発・検証環境 |
|---|---|---|
| バケット | 本番用のバケット | ローカル開発用のバケット |
| 公開ドメイン（`R2_PUBLIC_BASE_URL`） | `https://assets.natuportal.org` | `https://assets-dev.natuportal.org` |
| トークンの対象 | 本番用のバケットだけ | ローカル開発用のバケットだけ（検証環境用は別に発行） |
| CORS | `https://natueve-web.natuportal.org` からの `GET`・`PUT`・`HEAD` | `http://localhost:3000`・`http://localhost:8787` からの `GET`・`PUT`・`HEAD`（検証環境のフロントのオリジンも加える） |
| ライフサイクルルール | `natueve/tmp/` を 1 日後に削除 | 同じ |

| キー・URL | 値 |
|---|---|
| 画像・PDF の URL | `R2_PUBLIC_BASE_URL + "/" + オブジェクトキー`（`internal/service/object_url.go`） |
| 一時領域のキー | `natueve/tmp/{profileID}/{uuid}{ext}`（presign で発行。`internal/service/upload.go` の `PresignPut`） |
| 画像のキー | `natueve/{events\|reports}/images/{uuid}{ext}`（同 `buildFinalKey`） |
| PDF のキー | `natueve/{events\|reports}/documents/{uuid}{ext}`（同 `buildFinalKey`） |

- バケットの設定（公開ドメイン・CORS・ライフサイクルルール）はダッシュボードで行い、リポジトリには無い。
- トークンにクライアント IP アドレスの制限はかけない（ADR-0039）。
- 検証環境もローカル開発用のバケットを使う（ADR-0039）。
- 公開ドメインはバケット全体を配信する。一時領域のファイルも URL を知っていれば取得できる。
  - 一時領域の画像は、EXIF の位置情報を除去する前の元ファイル（除去は昇格時）。API は昇格後も元ファイルを消さない。
  - キーを受け取るのはアップロードした本人だけで、ランダムな UUID を含む。
  - ライフサイクルルールの削除は期限から通常 24 時間以内（公式）で、アップロードから通常 2 日程度は取得できる。
- アップロードから 1 日を過ぎた一時領域のキーは消えている場合があり、昇格に失敗する。

#### フロントの画像の許可リスト

フロント（NatuEve_Web_Frontend）の `next.config.ts` の `images.remotePatterns` は、画像最適化で変換する画像のホストとパスを絞っている。
API が返す画像の URL がこれに合わないと、フロントで画像が表示されない。

| `remotePatterns` の項目 | 値 | 対応する API 側の値 |
|---|---|---|
| `hostname` | `assets.natuportal.org`（ローカル開発・検証環境の `assets-dev.natuportal.org` は本番以外のビルドだけ。NatuEve_Web_Frontend#353） | `R2_PUBLIC_BASE_URL` |
| `pathname` | `/natueve/events/images/*`・`/natueve/reports/images/*`（予定。NatuEve_Web_Frontend#351） | 画像のキーの形式（`buildFinalKey`） |

次を変えるときは、フロントの `remotePatterns` も合わせて変える。

- 配信ドメイン（`R2_PUBLIC_BASE_URL`）を変える: `hostname`
- 画像のキーの形式（`buildFinalKey`）を変える: `pathname`

## 本番の環境変数

本番では `.env` を使わず、Cloud Run の環境変数として注入する。秘密情報は Secret Manager から渡す。
設定はデプロイのたびに CI が行う。値の置き場所は [設定値の置き場所](#設定値の置き場所)。

| 変数 | 本番の値 | 秘密情報 |
|---|---|---|
| `PORT` | Cloud Run が注入（設定不要） | |
| `GIN_MODE` | `release` | |
| `DATABASE_URL` | Supabase の Session pooler の接続文字列（末尾に `?sslmode=require`） | ○ |
| `DB_MAX_OPEN_CONNS` | `6`（1 プロセスあたりの DB 接続数の上限。ADR-0037） | |
| `DB_AUTO_MIGRATE` | `false` | |
| `SUPABASE_JWKS_URL` | `https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json` | |
| `SUPABASE_JWT_AUD` | `authenticated`（既定値のままなら設定不要） | |
| `TRUSTED_PROXIES` | 使わない（利用者 IP は Worker が渡すヘッダーから読む。ADR-0034） | |
| `RELAY_SECRET` | 中継元の秘密値（32 バイト以上のランダム値。例: `openssl rand -hex 32`）。Worker の `RELAY_SECRET` と同じ値 | ○ |
| `RELAY_SECRET_PREVIOUS` | 値の差し替え中だけ、古い値を設定する（[手順](#runapp-の-url-へ直接の大量アクセスを受けたとき)） | ○ |
| `RELAY_CLIENT_IP_HEADER` | `X-NatuEve-Client-IP`（Worker の `CLIENT_IP_HEADER` と同じ値） | |
| `CORS_ALLOW_ORIGINS` | `https://natueve-web.natuportal.org`（NatuEve_Web_Frontend#328 で決定） | |
| `R2_ACCOUNT_ID` | Cloudflare のアカウント ID | |
| `R2_ACCESS_KEY_ID` | R2 の API トークン（対象は本番用のバケットだけ） | ○ |
| `R2_SECRET_ACCESS_KEY` | R2 の API トークン（対象は本番用のバケットだけ） | ○ |
| `R2_BUCKET` | 本番用のバケット名（必ず設定する。未設定だと既定値 `natuportal` になる） | |
| `R2_PUBLIC_BASE_URL` | `https://assets.natuportal.org`（変えるときは[フロントの画像の許可リスト](#フロントの画像の許可リスト)も変える） | |
| `RESEND_API_KEY` | Resend の API キー | ○ |
| `MAIL_FROM` | 送信元アドレス | |
| `SCHEDULER_OIDC_AUDIENCE` | Scheduler のジョブに設定した OIDC の audience（API の run.app の URL） | |
| `SCHEDULER_SERVICE_ACCOUNT_EMAIL` | Scheduler のジョブが使うサービスアカウントのメールアドレス | |
| `OUTBOX_WORKER_ENABLED` | 設定しない（常駐ワーカーは本番では使わない） | |

- `RELAY_SECRET` と `RELAY_CLIENT_IP_HEADER` は両方設定する。片方だけ、または 32 バイト未満の値では API が起動しない。
  両方未設定でも起動するが、中継元を確かめず、利用者 IP も Worker のヘッダーから読まない（起動時に警告ログを出す）。
- `RELAY_SECRET_PREVIOUS` は、`RELAY_SECRET` と同じシークレットの古いバージョンを割り当てる。
- Secret Manager の無料枠は、アクティブなシークレットバージョン 6 つ・アクセス 1 万回／月。
  秘密情報は中継元の秘密値を含めて 5 つなので無料枠に収まる。値の差し替え中は中継元の秘密値が 2 バージョンになり 6 つになる。
  差し替えを終えたら古いバージョンを無効化する。
- `DB_MAX_OPEN_CONNS` は必ず設定する。未設定でも起動するが、接続数に上限がなくなる（起動時に警告ログを出す）。
  整数でない値・2 未満の値では起動しない。値は [Supabase](#supabase) の Pool Size から決める。
- `SCHEDULER_OIDC_AUDIENCE` と `SCHEDULER_SERVICE_ACCOUNT_EMAIL` は、どちらか一方でも未設定だと内部エンドポイントが登録されず（404）、
  通知メールは送信されない。`RESEND_API_KEY` または `MAIL_FROM` が未設定の場合も登録されない。

## マイグレーション

本番は `DB_AUTO_MIGRATE=false`。CI が API のデプロイの前に適用する（`deploy/migrate.sh`）。
手元から適用するときは次のとおり。

```bash
read -rs -p 'DATABASE_URL: ' DATABASE_URL; echo   # bash。本番 DB の接続文字列を入力する（履歴に残さない）
DATABASE_URL="$DATABASE_URL" make migrate-up GOOSE_EXEC='sh -c'
unset DATABASE_URL
```

- 順序は「マイグレーション → API のデプロイ」。
- 環境変数で渡した `DATABASE_URL` は、手元の `.env` の値より優先される。
  `export` せず、終わったら `unset` する（残ると、同じシェルの `make run` で手元の API が本番 DB につながる）。
- 詳細は [Database.md](./Database.md) を参照。

## デプロイ（CI/CD）

方式は [ADR-0040](./adr/0040-github-actions-deploy-separate-gcp-projects.md)（ADR-0036 の流れの具体化）。ワークフローは `.github/workflows/deploy.yml`。

1. main への push で CI（`ci.yml`）が成功すると、Deploy ワークフローが動く。
2. 検証環境: main の最新か確かめる → イメージをビルドして検証用の Artifact Registry に置く → マイグレーション → Cloud Run → Worker。
3. `production` 環境の承認を待つ。Actions の実行画面の「Review deployments」で承認する。マージした本人は承認できない。
4. 本番: main の最新か確かめる → イメージを本番用の Artifact Registry へコピー（ダイジェストは同じ）→ マイグレーション → Cloud Run → Worker。

- 承認待ちの間に次のマージがあると、古い実行の本番のジョブは失敗する。最新の実行を承認する。
  後のコミットで CI が失敗すると、それを直すコミットが入るまで本番に出せない（古い実行は main の最新でないため失敗し、最新のコミットではデプロイが始まらない）。
- 設定（変数・シークレット）だけを反映するときは、Actions の Deploy から main を対象に手動で実行する（Run workflow）。
- Cloud Run の環境変数・シークレットはデプロイのたびに置き換わる。ダッシュボードで手で足した値は、次のデプロイで消える。
- 初期設定が済むまで、main への push のたびに Deploy ワークフローは失敗する。

### 設定値の置き場所

| 置き場所 | 値 |
|---|---|
| `deploy/cloud-run/<環境>.yaml` | `GIN_MODE`・`DB_MAX_OPEN_CONNS`・`DB_AUTO_MIGRATE`・`RELAY_CLIENT_IP_HEADER`・`CORS_ALLOW_ORIGINS`・`R2_PUBLIC_BASE_URL` |
| GitHub Environments の変数 | 下の表 |
| GitHub Environments のシークレット | `CLOUD_RUN_SERVICE`・`SCHEDULER_OIDC_AUDIENCE`（run.app の URL を組み立てられる値）、`CLOUDFLARE_API_TOKEN` |
| Secret Manager（環境ごとのプロジェクト） | `DATABASE_URL`・`RELAY_SECRET`・`R2_ACCESS_KEY_ID`・`R2_SECRET_ACCESS_KEY`・`RESEND_API_KEY` |

GitHub Environments（`staging`・`production`）の変数:

| 変数 | 値 |
|---|---|
| `GCP_PROJECT_ID` | その環境の GCP プロジェクト ID |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | `projects/<プロジェクト番号>/locations/global/workloadIdentityPools/github/providers/github` |
| `GCP_DEPLOYER_SERVICE_ACCOUNT` | `deployer@<プロジェクト ID>.iam.gserviceaccount.com` |
| `GCP_RUNTIME_SERVICE_ACCOUNT` | `api-runtime@<プロジェクト ID>.iam.gserviceaccount.com` |
| `ARTIFACT_REGISTRY_REPOSITORY` | `natueve` |
| `SUPABASE_JWKS_URL` | その環境の Auth 用の Supabase プロジェクトの JWKS の URL |
| `R2_ACCOUNT_ID`・`CLOUDFLARE_ACCOUNT_ID` | Cloudflare のアカウント ID |
| `R2_BUCKET` | 本番: 本番用のバケット名。検証環境: ローカル開発用のバケット名（ADR-0039） |
| `MAIL_FROM` | 送信元アドレス。本番は `mail.natuportal.org`、検証環境は `dev-mail.natuportal.org` のアドレス（ADR-0041） |
| `SCHEDULER_SERVICE_ACCOUNT_EMAIL` | `scheduler@<プロジェクト ID>.iam.gserviceaccount.com` |
| `RELAY_SECRET_PREVIOUS_VERSION` | 中継元の秘密値の差し替え中だけ、`RELAY_SECRET` の古いバージョン番号 |

- `SCHEDULER_SERVICE_ACCOUNT_EMAIL`・`RELAY_SECRET_PREVIOUS_VERSION`・シークレットの `SCHEDULER_OIDC_AUDIENCE` 以外は必須で、未設定だとデプロイが止まる。

### 初期設定（1 回だけ）

検証環境と本番で、それぞれ行う。`<環境>` は `staging` または `production`。

1. **GCP のプロジェクト**: 作成して請求アカウントにつなぎ、API を有効にして、Artifact Registry のリポジトリを作る。

   ```bash
   PROJECT_ID=<プロジェクト ID>
   ENVIRONMENT=<環境>
   PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"
   gcloud services enable cloudresourcemanager.googleapis.com iam.googleapis.com run.googleapis.com \
     artifactregistry.googleapis.com secretmanager.googleapis.com iamcredentials.googleapis.com sts.googleapis.com \
     cloudscheduler.googleapis.com --project "$PROJECT_ID"
   gcloud artifacts repositories create natueve --repository-format=docker --location=asia-northeast1 --project "$PROJECT_ID"
   ```

2. **サービスアカウント**: `deployer`（CI）・`api-runtime`（Cloud Run）・`scheduler`（Cloud Scheduler）。

   ```bash
   for sa in deployer api-runtime scheduler; do gcloud iam service-accounts create "$sa" --project "$PROJECT_ID"; done
   DEPLOYER="deployer@${PROJECT_ID}.iam.gserviceaccount.com"
   RUNTIME="api-runtime@${PROJECT_ID}.iam.gserviceaccount.com"
   gcloud projects add-iam-policy-binding "$PROJECT_ID" --member="serviceAccount:${DEPLOYER}" --role=roles/run.admin
   gcloud iam service-accounts add-iam-policy-binding "$RUNTIME" --project "$PROJECT_ID" \
     --member="serviceAccount:${DEPLOYER}" --role=roles/iam.serviceAccountUser
   gcloud artifacts repositories add-iam-policy-binding natueve --location=asia-northeast1 --project "$PROJECT_ID" \
     --member="serviceAccount:${DEPLOYER}" --role=roles/artifactregistry.writer
   ```

   本番だけ、本番の `deployer` に検証用の Artifact Registry の読み取りを付ける（イメージのコピー元）。

   ```bash
   gcloud artifacts repositories add-iam-policy-binding natueve --location=asia-northeast1 --project <検証用のプロジェクト ID> \
     --member="serviceAccount:deployer@<本番のプロジェクト ID>.iam.gserviceaccount.com" --role=roles/artifactregistry.reader
   ```

3. **Secret Manager**: 秘密値を登録し、`api-runtime` に読み取りを、`deployer` に `DATABASE_URL` の読み取りを付ける。
   値は環境ごとに別にする。`RELAY_SECRET` は `openssl rand -hex 32` で作り、Worker にも同じ値を入れる（手順 8）。

   ```bash
   for name in DATABASE_URL RELAY_SECRET R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY RESEND_API_KEY; do
     read -rs -p "${name}: " value; echo
     printf '%s' "$value" | gcloud secrets create "$name" --data-file=- --project "$PROJECT_ID"
     gcloud secrets add-iam-policy-binding "$name" --project "$PROJECT_ID" \
       --member="serviceAccount:${RUNTIME}" --role=roles/secretmanager.secretAccessor
   done
   unset value
   gcloud secrets add-iam-policy-binding DATABASE_URL --project "$PROJECT_ID" \
     --member="serviceAccount:${DEPLOYER}" --role=roles/secretmanager.secretAccessor
   ```

4. **Workload Identity**: GitHub Actions のその環境のジョブだけが `deployer` を使えるようにする。

   ```bash
   gcloud iam workload-identity-pools create github --location=global --project "$PROJECT_ID"
   gcloud iam workload-identity-pools providers create-oidc github --location=global --workload-identity-pool=github \
     --project "$PROJECT_ID" --issuer-uri="https://token.actions.githubusercontent.com" \
     --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.repository_id=assertion.repository_id" \
     --attribute-condition="assertion.repository == 'GokujyouKaisennDonnburi/NatuEve_API' && assertion.repository_id == '1270840941'"
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOYER" --project "$PROJECT_ID" --role=roles/iam.workloadIdentityUser \
     --member="principal://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/github/subject/repo:GokujyouKaisennDonnburi/NatuEve_API:environment:${ENVIRONMENT}"
   ```

5. **Supabase**（検証環境だけ）: 個人アカウントに検証用 DB のプロジェクトを作る。Auth はローカル開発の Auth 用プロジェクトを使う（ADR-0041）。
   DB のプロジェクトは Data API を無効にし、Session pooler の接続文字列（末尾に `?sslmode=require`）を `DATABASE_URL` に入れる。Pool Size を確かめる（[Supabase](#supabase)）。
6. **R2・Resend**: 手順 3 で登録する値を用意する。
   - R2: 環境ごとにトークンを発行する。本番は本番用のバケットだけ、検証環境はローカル開発用のバケットだけを対象にする（ADR-0039）。検証環境のトークンはローカル開発のトークンと別にする。
   - Resend: 本番は送信元 `mail.natuportal.org` のキー、検証環境はローカル開発と共用のキー（送信元 `dev-mail.natuportal.org`）を使う（ADR-0041）。
7. **GitHub Environments**（Settings → Environments）: `staging` と `production` を作り、[変数とシークレット](#設定値の置き場所)を入れる。
   - 両方: Deployment branches を `main` だけにする。
   - `production`: Required reviewers に承認者を入れ、Prevent self-review を有効にする。管理者が保護ルールを迂回できる設定は無効にする。
   - `CLOUD_RUN_SERVICE` は推測されにくい名前にする（例: `natueve-api-` にランダムな文字列を足す。[run.app の URL へ直接の大量アクセスを受けたとき](#runapp-の-url-へ直接の大量アクセスを受けたとき)）。
     `SCHEDULER_OIDC_AUDIENCE` は `https://<CLOUD_RUN_SERVICE>-<プロジェクト番号>.asia-northeast1.run.app`。
8. **Cloudflare**: Worker の secret を環境ごとに入れる（[Cloudflare Workers](#cloudflare-workersapi-の中継)）。Worker が無ければこのとき作られる。
   `ORIGIN_URL` は `SCHEDULER_OIDC_AUDIENCE` と同じ値。
   続けて API トークンを環境ごとに作り、`CLOUDFLARE_API_TOKEN` に入れる。ポリシーは次の 2 つだけにする。
   - 「指定された Workers」: その環境の Worker に Editor。Worker は作った後でないと選べない。
   - ゾーン（`natuportal.org`）: Workers Routes の編集。

   テンプレート「Edit Cloudflare Workers」にあるアカウント全体のポリシーは付けない（付けると、アカウント内のすべての Worker を変更できる）。
   Custom Domain 付きのデプロイに、Zone の DNS の編集権限は要らない。
   この 2 つのポリシーで検証環境のデプロイが通ること、そのトークンで本番の Worker のデプロイ履歴（`wrangler deployments list`）が拒否されることを確かめた（#254）。
9. **初回のデプロイ**: main へのマージ、または手動の実行で Deploy ワークフローを動かす。
10. **Cloud Scheduler**: 初回のデプロイの後に、ジョブを作って手動で実行し、ログで 200 になることを確かめる（[Cloud Scheduler](#cloud-scheduler)）。
    失敗した呼び出しは、1 分後の呼び出しで処理される。

    ```bash
    read -rs -p "SCHEDULER_OIDC_AUDIENCE: " RUN_URL; echo
    gcloud scheduler jobs create http natueve-outbox --location=asia-northeast1 --project "$PROJECT_ID" \
      --schedule="* * * * *" --uri="${RUN_URL}/internal/outbox/process" --http-method=POST \
      --oidc-service-account-email="scheduler@${PROJECT_ID}.iam.gserviceaccount.com" --oidc-token-audience="${RUN_URL}" \
      --max-retry-attempts=0 --format='value(name,state)'
    unset RUN_URL
    gcloud scheduler jobs run natueve-outbox --location=asia-northeast1 --project "$PROJECT_ID"
    # 1〜2 分後に応答のステータスを見る
    gcloud logging read 'resource.type="cloud_run_revision" AND httpRequest.requestUrl:"/internal/outbox/process"' \
      --project "$PROJECT_ID" --limit 5 --freshness=10m --format='table(timestamp,httpRequest.status)'
    ```

11. **確認**: [独自ドメイン経由の検証](#独自ドメイン経由の検証226)の表を確かめる。検証環境のドメインは `natueve-api-stg.natuportal.org`。

## 月額の試算

2026-10 時点の試算。現行のメール基盤（Resend）の場合。

| 規模 | 月間 PV | API リクエスト | メール | R2 の画像容量 |
|---|---|---|---|---|
| 小（公開初期） | 3 万 | 10 万 | 1,000 通 | 5GB |
| 中 | 30 万 | 100 万 | 1 万通 | 30GB |
| 大 | 300 万 | 1,000 万 | 10 万通 | 200GB |

| 項目（USD） | 小 | 中 | 大 |
|---|---|---|---|
| Cloud Run | 0 | 0 | 約 23 |
| Cloud Run から外への通信 | 約 0.06 | 約 0.6 | 約 6 |
| Cloud Scheduler | 0 | 0 | 0 |
| Cloudflare Workers（API の中継） | 0 | 0 | 0〜約 3.4 |
| Cloudflare R2 | 0 | 約 0.3 | 約 4.7 |
| Resend | 0（Free） | 20（Pro） | 35（Pro） |
| Supabase | 0（Free）／25（Pro） | 25 | 25〜 |
| **合計** | **約 0〜25** | **約 46** | **約 94〜97** |

試算の前提:

- Supabase の中・大の値は Pro にした場合の参考値。本番も Free のまま運用する（ADR-0036）が、DB が 500MB を超える規模では Free に収まらない。
- 検証環境（ADR-0036）の費用は含まない。
- API は 1 リクエストあたり処理 0.1 秒・応答 5KB、1 vCPU・512MiB。同時に処理したリクエストの時間を重ねずに足しているため、Cloud Run は多めの見積もり。
- 1 PV あたり画像を 5 枚読み込む。Scheduler は 1 分ごと（月 約 4.3 万回）。
- Cloud Run の無料枠（請求アカウント単位、リクエスト課金）: 月 200 万リクエスト・18 万 vCPU 秒・36 万 GiB 秒。
- Cloud Run の超過単価は第三者サイトの us-central1 の値（vCPU 秒 $0.000024・GiB 秒 $0.0000025・100 万リクエスト $0.40）。東京の単価は未確認。
- Cloud Run の CPU・メモリの費用の上限は、最大インスタンス数 1（ADR-0037）で、1 台が 1 か月休まず処理し続けた場合の 約 $60（無料枠を引いた値）。
  大量のアクセスを受け続けた場合の値で、上の表には含めない。リクエストの件数による課金は、この上限とは別にかかる（ADR-0034）。
- 外への通信は、アジア向けの単価 $0.12/GiB（2024 年の料金改定告知の値）で計算した。
- API の独自ドメインは Workers の中継（ADR-0034）で、ロードバランサー（転送ルールだけで月 約 $18）は使わない。
  中継はフロントと共有する Workers Paid（月 $5、フロント側の試算に計上）の含まれる量を使う。
  大規模はフロントが含まれる量を使い切った場合の超過分（1,000 万リクエスト × $0.30/100 万件、CPU は 1 リクエスト 2 ミリ秒と仮定）。
- 大規模で Supabase のコンピュート増強が必要になるかは未確認。大規模の合計は、Workers の超過分の有無で幅を持たせた値で、
  Supabase のコンピュート増強分は含まない。

## 未決事項・今後の作業

1. **Cloud Scheduler ジョブの作成**（outbox の内部エンドポイントは #217、ADR-0035 で実装済み）
   - `POST /internal/outbox/process` を 1 分間隔で呼ぶジョブを作る（手順は[初期設定](#初期設定1-回だけ)の 10）。認証は OIDC トークンで、ジョブ用のサービスアカウントと
     audience を設定し、同じ値を Cloud Run の環境変数 `SCHEDULER_SERVICE_ACCOUNT_EMAIL`・`SCHEDULER_OIDC_AUDIENCE` に入れる。
   - **ジョブを作るまで、本番では通知メールが送信されない。** 常駐ワーカーは本番では使わない（`OUTBOX_WORKER_ENABLED` を設定しない）。
   - 複数の呼び出しが同時に走っても、行ごとの `FOR UPDATE SKIP LOCKED` で排他するため、同じ通知を同時に送ることはない。ただし、送信後に記録できなかった場合と、送信を打ち切ったが実際には届いていた場合は再送されうる（at-least-once）。
   - ローカル開発では `OUTBOX_WORKER_ENABLED=true`（`.env.example` に記載）で、API プロセス内の 30 秒ティッカーが同じ処理を呼ぶ。
2. **Supabase への接続方式**（#213・#210、確認済み）: Session pooler（ポート 5432）。
   - 直接接続は IPv6 のみ（IPv4 は有料アドオン）。トランザクションモードのプーラー（ポート 6543）は prepared statement に対応していない。
   - 本番 DB へのマイグレーション（goose。pgx/stdlib 経由の database/sql で、1 件ずつトランザクションで適用）が Session pooler 経由で通った。
3. **Supabase で PostgreSQL 17 と `pg_trgm` が使えるかの確認**（#213・#210、確認済み）: PostgreSQL 17.11。`CREATE EXTENSION pg_trgm` を含むマイグレーションが通った。
4. **DB を Auth と同じ Supabase プロジェクトに置くか、別プロジェクトにするか**（#214）
   - Auth は NatuPortal の複数プロダクトで共有しているため、Data API を無効化すると他プロダクトに影響しうる。
   - 別プロジェクトにすると、Pro でもコンピュート費用（Micro で月 $10）が追加される。
   - 本番 DB のプロジェクトでは Data API を無効にした（#214）。
5. **CORS の許可オリジンの値**: 環境変数 `CORS_ALLOW_ORIGINS` で設定できる（#218、ADR-0033）。
   本番の値は `https://natueve-web.natuportal.org`（NatuEve_Web_Frontend#328 で決定）。
   未設定のままだと開発用の `http://localhost:3000`・`http://localhost:8787` だけが許可され、本番のフロントから API を呼べない。
   プレビュー環境の URL を許可するか（NatuEve_Web_Frontend#330）は未決。ワイルドカードは使えないため、許可する場合は完全一致の値を追加する。
6. **CI/CD**（#219、ADR-0040 で決定）: GitHub Actions で、main へのマージで検証環境へ、承認後に同じイメージを本番へ出す（[デプロイ（CI/CD）](#デプロイcicd)）。
   [初期設定](#初期設定1-回だけ)の手順 1〜8 は検証環境・本番とも、手順 9〜11 は検証環境だけ済み（#254）。本番の手順 9〜11 は #209。
7. **利用者 IP の取得方式の検証**（#215）: ADR-0034 で、Worker が渡すヘッダーを Gin の `TrustedPlatform` で読む方式に決めた。
   本番では `TRUSTED_PROXIES` を使わない。独自ドメイン経由で利用者 IP が正しく取れ、偽装できないことを検証する。
   参加申込の IP レート制限（`internal/middleware/ratelimit.go`）は `c.ClientIP()` ごとに数えるため、これが正しいことが前提。
8. **停止時の待ち時間**（#216）: 現在の `cmd/api/main.go` は、HTTP サーバーの停止待ち（最大 10 秒）とワーカーの停止待ち（最大 10 秒）を順に行う。
   合計が Cloud Run の猶予（10 秒）を超えうる。
9. **最大インスタンス数**（#240、ADR-0037 で決定）: サービス単位で 1 にする。1 プロセスあたりの DB 接続数の上限 `DB_MAX_OPEN_CONNS` は 6。
   - CI がデプロイのたびに `--max 1` を指定する（`deploy/cloud-run-deploy.sh`）。検証環境も同じ値。
   - 本番の Supabase の Pool Size は 15 で、条件を満たす（[Supabase](#supabase)）。
   - 最大インスタンス数で抑えられるのは CPU・メモリの費用で、run.app の URL への直接の大量アクセスによる件数の課金は抑えられない（ADR-0034）。
   - 最大インスタンス数を上げる場合は、IP レート制限の緩み（インスタンスが N 台あると最大 N 倍）と、DB の接続数の関係（ADR-0037 の決定事項 4）を見直す。
10. **API の中継の検証**（#226）: 方式は ADR-0034 で決めた（Workers の中継、秘密ヘッダーで中継元を確認）。
    サブドメインは `natueve-api.natuportal.org`。中継用 Worker（`workers/api-relay/`）と API 側の確認・利用者 IP の読み取り
    （`internal/middleware/relay.go`）は実装済み。
    [独自ドメイン経由の検証](#独自ドメイン経由の検証226)は、検証環境（`natueve-api-stg.natuportal.org`）で済み（#254）。
    CORS とフロントの SSR は、検証環境のフロントのオリジンが決まっていないため未確認。本番のドメインでは本番への初回のデプロイ（#209）の後に確かめ、結果をここに反映する。
    検証で成り立たなければロードバランサーに切り替える（ADR-0034 の決定事項 7）。
11. **予算アラート**: Cloud Billing の予算アラートを設定する。金額は未決。
    run.app の URL への直接の大量アクセスによる費用の急増を、[下記](#runapp-の-url-へ直接の大量アクセスを受けたとき)の手順の起点として検知する。

## run.app の URL へ直接の大量アクセスを受けたとき

ADR-0034 の構成では、秘密ヘッダーのないリクエストも Cloud Run のコンテナまで届き、件数に応じて課金される。

1. **検知**: 予算アラート、または Cloud Run のリクエスト数と、秘密ヘッダーの確認で拒否した件数の急増。
   拒否したリクエストは、API のログに `中継元の確認に失敗したリクエストを拒否しました`（Warn）として出る。
2. **一時的な対処**: Cloud Run のサービスを別名で作り直し、新しい run.app の URL を Worker の転送先（`wrangler secret put ORIGIN_URL`）に設定してから、
   古いサービスを削除する。Cloud Scheduler の呼び出し先も新しい URL に変える。
   作り直しは、GitHub Environments のシークレット `CLOUD_RUN_SERVICE`・`SCHEDULER_OIDC_AUDIENCE` を変えて Deploy ワークフローを手動で実行する。run.app の URL はサービス名を含み、
   プロジェクト番号は秘密ではないため、新しい名前はランダムな文字列を含む推測されにくいものにする。新しい URL を知られれば効果はない。
3. **恒久的な対処**（ADR-0034 の決定事項 7）: Cloud Run の IAM 認証を追加し、Worker が ID トークンを付けて呼ぶ。
   または外部ロードバランサーに切り替え、Cloud Run の ingress を「内部と Cloud Load Balancing」に制限する。
   いずれも実装・設定の作業が必要で、終えるまでの間は課金が続く。
4. **秘密ヘッダーの値が漏れた疑いがある場合**: 値を差し替える。次の順で行う。
   1. Secret Manager の `RELAY_SECRET` に新しいバージョンを追加し、GitHub Environments の変数 `RELAY_SECRET_PREVIOUS_VERSION` に古いバージョンの番号を入れて、
      Deploy ワークフローを手動で実行する（API の `RELAY_SECRET` は最新、`RELAY_SECRET_PREVIOUS` は古いバージョンになる）。
   2. Worker を新しい値に切り替える（`wrangler secret put RELAY_SECRET --env=""`）。
   3. 変数 `RELAY_SECRET_PREVIOUS_VERSION` を消して Deploy ワークフローを手動で実行し、Secret Manager の古いバージョンを無効化する。

## 独自ドメイン経由の検証（#226）

API と Worker をデプロイした後に行う。結果は検証環境が #254、本番が #209 に返信し、[未決事項](#未決事項今後の作業) 10 に反映する。
`<run.app>` は Cloud Run の URL、`<フロント>` は `CORS_ALLOW_ORIGINS` に入れたオリジン。

| 確認すること | 手順 | 期待する結果 |
|---|---|---|
| HTTPS で届く | `curl -sS https://natueve-api.natuportal.org/health` | `{"status":"ok"}` |
| 認証付きの API | `curl -sS -H "Authorization: Bearer <JWT>" https://natueve-api.natuportal.org/api/v1/me` | 200 |
| CORS のプリフライト | `curl -sS -i -X OPTIONS -H "Origin: <フロント>" -H "Access-Control-Request-Method: POST" -H "Access-Control-Request-Headers: authorization,content-type" https://natueve-api.natuportal.org/api/v1/events` | 204、`Access-Control-Allow-Origin: <フロント>` |
| run.app への直接アクセス | `curl -sS -i https://<run.app>/api/v1/events` | 403（`/health` は 200） |
| `workers.dev`・Preview URL | ダッシュボードの Worker の「Domains & Routes」で無効になっていること。`https://<Worker 名>.<アカウントのサブドメイン>.workers.dev/health`（Worker 名は本番が `natueve-api-relay`、検証環境が `natueve-api-relay-stg`） | 届かない |
| Pseudo IPv4 | ダッシュボードのゾーンの「Network」の Pseudo IPv4 | Off |
| キャッシュルール | ゾーンの「Caching」の Cache Rules と Page Rules に、`natueve-api.natuportal.org` に当たるルールがないこと | ない |
| フロントの SSR から呼べる | フロントの `NEXT_PUBLIC_API_BASE_URL` を `https://natueve-api.natuportal.org` にして SSR のページを開く | データが表示される。API のログの `ip` の値を #215 のために記録する |
| 応答時間 | `curl -sS -o /dev/null -w '%{time_starttransfer}\n'` で、独自ドメインと run.app の `/health` を各 20 回程度 | 中央値の差を記録する |
| WAF・レート制限 | ゾーンに検証用のカスタムルール（例: パスが `/__waf-test` ならブロック）を一時的に作り、独自ドメインの `/__waf-test` を呼ぶ。レート制限ルールも同様に確かめ、終わったら削除する | Cloudflare がブロックする（API のログに出ない） |

- 利用者 IP が正しく取れることと偽装できないこと、IP レート制限の検証は #215 で行う（方法は [ADR-0038](./adr/0038-ip-verification-via-api.md)）。
- 秘密値をコマンドラインに書かない（シェルの履歴に残る）。

## メールの将来的な移行

### 現状

- 送信基盤は Resend（ADR-0001）。Free プランは月 3,000 通・**1 日 100 通まで**。
- 送信処理は `Mailer` インターフェース（`internal/service/event_notification.go`）の裏にある。
  送信サービスを替えても、outbox（ADR-0016）と Cloud Scheduler による起動はそのまま使える。

### 移行を検討する条件

- 1 日の送信数が 100 通に届く。参加者 100 人を超えるイベントを 1 回中止するだけで到達しうる（ADR-0016 のトレードオフ参照）。
- 月の送信数が 3,000 通を超える。

### 移行先の候補

| 候補 | 料金 | 小（1,000 通） | 中（1 万通） | 大（10 万通） | 必要な作業・注意 |
|---|---|---|---|---|---|
| Resend Pro | $20/月（5 万通）、$35/月（10 万通） | 0（Free のまま） | 20 | 35 | プラン変更のみ |
| Amazon SES | 1,000 通あたり $0.10（添付は 1GB あたり $0.12） | 約 0.1 | 約 1 | 約 10 | AWS アカウント作成、ドメイン認証（DKIM）、サンドボックス解除の申請、バウンス処理の設定 |
| Cloudflare Email Service | 月 3,000 通込み、超過は 1,000 通あたり $0.35。Workers Paid（$5/月）が必須 | 5 | 約 7.5 | 約 39 | 送信機能はベータ版。REST API・SMTP・Go SDK で外部のサーバーから送信できる。1 日あたりの上限は未確認 |

（金額は月額 USD。Cloudflare Email Service は Workers Paid の $5 を含む。）

### 移行手順

1. 移行先を決め、ADR を起こす（ADR-0001 を置き換える）。
   Resend 固有の ADR（ADR-0005 レート制限、ADR-0006 User-Agent）も見直す。
2. 移行先の `Mailer` 実装を追加し、設定（環境変数）で切り替えられるようにする。
3. 送信用サブドメイン（ADR-0002）で、移行先のドメイン認証（SPF・DKIM・DMARC）を設定する。
4. 本番の環境変数・シークレットを差し替えてデプロイする。outbox に溜まっている `pending` 行は、切り替え後の実装で送信される。

## 参考（料金・仕様の出典）

- Cloud Run: [billing settings](https://docs.cloud.google.com/run/docs/configuring/billing-settings) / [CPU limits](https://docs.cloud.google.com/run/docs/configuring/services/cpu) / [pricing](https://cloud.google.com/run/pricing) / [max instances](https://docs.cloud.google.com/run/docs/configuring/max-instances) / [about maximum instances](https://docs.cloud.google.com/run/docs/configuring/max-instances-limits) / [instance autoscaling](https://docs.cloud.google.com/run/docs/about-instance-autoscaling) / [concurrency](https://docs.cloud.google.com/run/docs/about-concurrency) / [secrets](https://docs.cloud.google.com/run/docs/configuring/services/secrets)
- Google Cloud: [Free Tier](https://docs.cloud.google.com/free/docs/free-cloud-features) / [Cloud Scheduler pricing](https://cloud.google.com/scheduler/pricing) / [VPC pricing（egress 料金改定の告知）](https://cloud.google.com/vpc/pricing-announce)
- Supabase: [Pricing](https://supabase.com/pricing) / [Regions](https://supabase.com/docs/guides/platform/regions) / [Connecting to Postgres](https://supabase.com/docs/guides/database/connecting-to-postgres) / [Securing your API](https://supabase.com/docs/guides/api/securing-your-api) / [Compute and Disk](https://supabase.com/docs/guides/platform/compute-and-disk) / [Pooling and limits](https://supabase.com/docs/guides/database/connecting-to-postgres/pooling-and-limits) / [Supavisor FAQ](https://supabase.com/docs/guides/troubleshooting/supavisor-faq-YyP5tI)
- Cloud Run の独自ドメイン: [Mapping custom domains](https://docs.cloud.google.com/run/docs/mapping-custom-domains) / [Cloud Load Balancing pricing](https://cloud.google.com/load-balancing/pricing) / [Ingress](https://docs.cloud.google.com/run/docs/securing/ingress)
- Cloudflare: [R2 pricing](https://developers.cloudflare.com/r2/pricing/) / [R2 public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/) / [R2 object lifecycles](https://developers.cloudflare.com/r2/buckets/object-lifecycles/) / [Email Service pricing](https://developers.cloudflare.com/email-service/platform/pricing/)
- Cloudflare Workers: [Pricing](https://developers.cloudflare.com/workers/platform/pricing/) / [Custom Domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/) / [workers.dev](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/) / [Protecting your origin server](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/) / [HTTP headers](https://developers.cloudflare.com/fundamentals/reference/http-headers/)
- メール: [Resend pricing](https://resend.com/pricing) / [Amazon SES pricing](https://aws.amazon.com/ses/pricing/)
