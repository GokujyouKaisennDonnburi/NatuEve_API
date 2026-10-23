# デプロイ構成（バックエンド）

NatuEve_API の本番デプロイ構成をまとめる。
構成を選んだ理由と退けた代替案は [ADR-0032](./adr/0032-production-deployment-cloud-run-supabase.md) を参照。
API の独自ドメインと中継の方式は [ADR-0034](./adr/0034-api-custom-domain-workers-relay.md) を参照。
料金・仕様は 2026-10 時点で各公式ページを確認した値（出典は末尾）。

## 使用するサービス

| サービス | 用途 | 状態 |
|---|---|---|
| **Google Cloud Run**（`asia-northeast1`） | API コンテナの実行 | 確定 |
| **Google Cloud Scheduler** | outbox 処理エンドポイントの定期呼び出し（1 分間隔） | 確定 |
| **Supabase**（`ap-northeast-1`） | PostgreSQL 17（DB） | 確定 |
| **Supabase Auth** | 認証（JWT 発行・JWKS による検証） | 確定（既存） |
| **Cloudflare Workers** | API の独自ドメインの中継（[ADR-0034](./adr/0034-api-custom-domain-workers-relay.md)） | 確定（`workers/api-relay/`。独自ドメイン経由の検証は #226） |
| Cloudflare R2 | 画像・PDF の保存と配信 | 既存 |
| Resend | トランザクションメールの送信（[ADR-0001](./adr/0001-resend-transactional-email.md)） | 既存 |
| Google Artifact Registry | コンテナイメージの保管 | 予定 |
| Google Secret Manager | 秘密情報の注入 | 予定 |

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
| 最大インスタンス数 | 未決（[未決事項](#未決事項今後の作業)） |
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
- **デプロイ後に確認する（未検証）**: Scheduler が付けるトークンに `email` と `email_verified` が入っているか、`iss` の値は何かを、
  公式ドキュメントでは確認できていない。`gcloud scheduler jobs run <ジョブ名>` で手動実行し、ログで 200 になることを確かめる。
  401 なら audience、403 ならサービスアカウントのメールアドレスか `email` クレームの有無を疑う。

### Cloudflare Workers（API の中継）

| 項目 | 値 |
|---|---|
| プラン | Workers Paid（フロントと共有。NatuEve_Web_Frontend ADR-0001） |
| Worker 名 | `natueve-api-relay` |
| 割り当て | Custom Domain `natueve-api.natuportal.org` |
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
- デプロイの自動化（#219）までは手動で行う。API を先にデプロイし、`RELAY_SECRET`・`RELAY_CLIENT_IP_HEADER` を設定してから Worker をデプロイする。

```bash
cd workers/api-relay
bun install --frozen-lockfile
bunx wrangler secret put ORIGIN_URL     # https://<サービス名>-<プロジェクト番号>.asia-northeast1.run.app
bunx wrangler secret put RELAY_SECRET   # API の RELAY_SECRET と同じ値
bunx wrangler deploy
```

- 初回（Worker がまだ無い状態）で `wrangler secret put` から始める手順が通るかは未確認。通らない場合は、`wrangler deploy --secrets-file <ファイル>` で
  secret とデプロイを同時に渡す（ファイルはコミットしない）。

### Supabase

| 項目 | 値 |
|---|---|
| リージョン | `ap-northeast-1`（東京） |
| プラン | Free（本番公開後も Pro にしない。[ADR-0036](./adr/0036-github-flow-staging-approval.md)） |
| Data API（PostgREST） | NatuEve のテーブルに到達できない状態にする（無効化、または API 用ロールへ権限を付与しない） |
| Storage | 使わない（ファイルは Cloudflare R2） |

- Free プランの制限: DB 500MB、自動バックアップなし、1 週間アクティビティがないと一時停止、アクティブなプロジェクトは 2 つまで
  （組織ごとではなくユーザーごとに数え、Owner / Administrator になっている全組織を合算する）。
- Pro プラン（$25/月〜）: 日次バックアップ（7 日保持）。コンピュート Micro（1GB RAM）1 つ分のクレジット（$10/月）を含む。
- 既定では `public` スキーマに作ったテーブルに、API 用ロールの SELECT／INSERT／UPDATE／DELETE 権限が付く。
  RLS のないテーブルは、権限を持つロールなら誰でも読み書きできる。

## 本番の環境変数

本番では `.env` を使わず、Cloud Run の環境変数として注入する。秘密情報は Secret Manager から渡す。

| 変数 | 本番の値 | 秘密情報 |
|---|---|---|
| `PORT` | Cloud Run が注入（設定不要） | |
| `GIN_MODE` | `release` | |
| `DATABASE_URL` | Supabase の接続文字列（接続方式は未決） | ○ |
| `DB_AUTO_MIGRATE` | `false` | |
| `SUPABASE_JWKS_URL` | `https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json` | |
| `SUPABASE_JWT_AUD` | `authenticated`（既定値のままなら設定不要） | |
| `TRUSTED_PROXIES` | 使わない（利用者 IP は Worker が渡すヘッダーから読む。ADR-0034） | |
| `RELAY_SECRET` | 中継元の秘密値（32 バイト以上のランダム値。例: `openssl rand -hex 32`）。Worker の `RELAY_SECRET` と同じ値 | ○ |
| `RELAY_SECRET_PREVIOUS` | 値の差し替え中だけ、古い値を設定する（[手順](#runapp-の-url-へ直接の大量アクセスを受けたとき)） | ○ |
| `RELAY_CLIENT_IP_HEADER` | `X-NatuEve-Client-IP`（Worker の `CLIENT_IP_HEADER` と同じ値） | |
| `CORS_ALLOW_ORIGINS` | 本番のフロントのオリジン（未決。フロント側 NatuEve_Web_Frontend#328 で決まる） | |
| `R2_ACCOUNT_ID` | Cloudflare のアカウント ID | |
| `R2_ACCESS_KEY_ID` | R2 の API トークン | ○ |
| `R2_SECRET_ACCESS_KEY` | R2 の API トークン | ○ |
| `R2_BUCKET` | バケット名 | |
| `R2_PUBLIC_BASE_URL` | 公開配信のベース URL | |
| `RESEND_API_KEY` | Resend の API キー | ○ |
| `MAIL_FROM` | 送信元アドレス | |
| `SCHEDULER_OIDC_AUDIENCE` | Scheduler のジョブに設定した OIDC の audience（API の URL） | |
| `SCHEDULER_SERVICE_ACCOUNT_EMAIL` | Scheduler のジョブが使うサービスアカウントのメールアドレス | |
| `OUTBOX_WORKER_ENABLED` | 設定しない（常駐ワーカーは本番では使わない） | |

- `RELAY_SECRET` と `RELAY_CLIENT_IP_HEADER` は両方設定する。片方だけ、または 32 バイト未満の値では API が起動しない。
  両方未設定でも起動するが、中継元を確かめず、利用者 IP も Worker のヘッダーから読まない（起動時に警告ログを出す）。
- `RELAY_SECRET_PREVIOUS` は、`RELAY_SECRET` と同じシークレットの古いバージョンを割り当てる。
- Secret Manager の無料枠は、アクティブなシークレットバージョン 6 つ・アクセス 1 万回／月。
  秘密情報は中継元の秘密値を含めて 5 つなので無料枠に収まる。値の差し替え中は中継元の秘密値が 2 バージョンになり 6 つになる。
  差し替えを終えたら古いバージョンを無効化する。
- `SCHEDULER_OIDC_AUDIENCE` と `SCHEDULER_SERVICE_ACCOUNT_EMAIL` は、どちらか一方でも未設定だと内部エンドポイントが登録されず（404）、
  通知メールは送信されない。`RESEND_API_KEY` または `MAIL_FROM` が未設定の場合も登録されない。

## マイグレーション

本番は `DB_AUTO_MIGRATE=false`。新しいリビジョンをデプロイする前に、マイグレーションを適用する。

```bash
DATABASE_URL=... make migrate-up GOOSE_EXEC='sh -c'
```

- 順序は「マイグレーション → API のデプロイ」。
- 詳細は [Database.md](./Database.md) を参照。

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
- 外への通信は、アジア向けの単価 $0.12/GiB（2024 年の料金改定告知の値）で計算した。
- API の独自ドメインは Workers の中継（ADR-0034）で、ロードバランサー（転送ルールだけで月 約 $18）は使わない。
  中継はフロントと共有する Workers Paid（月 $5、フロント側の試算に計上）の含まれる量を使う。
  大規模はフロントが含まれる量を使い切った場合の超過分（1,000 万リクエスト × $0.30/100 万件、CPU は 1 リクエスト 2 ミリ秒と仮定）。
- 大規模で Supabase のコンピュート増強が必要になるかは未確認。大規模の合計は、Workers の超過分の有無で幅を持たせた値で、
  Supabase のコンピュート増強分は含まない。

## 未決事項・今後の作業

1. **Cloud Scheduler ジョブの作成**（outbox の内部エンドポイントは #217、ADR-0035 で実装済み）
   - `POST /internal/outbox/process` を 1 分間隔で呼ぶジョブを作る。認証は OIDC トークンで、ジョブ用のサービスアカウントと
     audience を設定し、同じ値を Cloud Run の環境変数 `SCHEDULER_SERVICE_ACCOUNT_EMAIL`・`SCHEDULER_OIDC_AUDIENCE` に入れる。
   - **ジョブを作るまで、本番では通知メールが送信されない。** 常駐ワーカーは本番では使わない（`OUTBOX_WORKER_ENABLED` を設定しない）。
   - 複数の呼び出しが同時に走っても、行ごとの `FOR UPDATE SKIP LOCKED` で排他するため、同じ通知を同時に送ることはない。ただし、送信後に記録できなかった場合と、送信を打ち切ったが実際には届いていた場合は再送されうる（at-least-once）。
   - ローカル開発では `OUTBOX_WORKER_ENABLED=true`（`.env.example` に記載）で、API プロセス内の 30 秒ティッカーが同じ処理を呼ぶ。
2. **Supabase への接続方式**（#213）
   - 接続方式の種類
     - 直接接続は IPv6 のみ（IPv4 は有料アドオン）。
     - セッションモードのプーラー（ポート 5432）は IPv4 で使える。
     - トランザクションモードのプーラー（ポート 6543）は prepared statement に対応していない。
   - セッションモードのプーラー（SessionPooler、ポート5432）を第一候補とする
     - IPv4 で接続でき、pgx（pgx/stdlib 経由の database/sql）の既定動作で使用される prepared statement にも対応しているため。
     - 実際の接続・クエリ・トランザクションの動作確認は別 Issue で実施する。
   - 接続手順
     - Supabase Dashboard の Connect から Session Pooler の接続情報（ホスト名、ポート、ユーザー名、データベース名）を取得する。
     - 接続情報を用いて DATABASE_URL を設定し、Cloud Run から接続する。認証情報は Secret Manager などで安全に管理する。
     - 接続先の PostgreSQL バージョンが 17 であること、および pg_trgm 拡張が利用可能であることを確認する。
     - pgx/stdlib 経由の database/sql でクエリとトランザクションを実行し、Goose によるマイグレーションの適用を確認する。
   - pgx（`pgx/stdlib` 経由の `database/sql`）の既定動作との組み合わせを検証して決める。
3. **Supabase で PostgreSQL 17 と `pg_trgm` が使えるかの確認**（ADR-0030 のインデックスが前提、#213）。
   - Supabase の PostgreSQL バージョンが 17 であること、および pg_trgm 拡張が利用可能であることを確認する(未確認)
   - 実際のバージョン確認と、pg_trgm を使用するマイグレーションの適用確認は別 Issue で実施する。
4. **DB を Auth と同じ Supabase プロジェクトに置くか、別プロジェクトにするか**（#214）
   - Auth は NatuPortal の複数プロダクトで共有しているため、Data API を無効化すると他プロダクトに影響しうる。
   - 別プロジェクトにすると、Pro でもコンピュート費用（Micro で月 $10）が追加される。
5. **CORS の許可オリジンの値**: 環境変数 `CORS_ALLOW_ORIGINS` で設定できる（#218、ADR-0033）。
   本番の値は、フロントのオリジン（NatuEve_Web_Frontend#328）が決まり次第設定する。
   未設定のままだと開発用の `http://localhost:3000`・`http://localhost:8787` だけが許可され、本番のフロントから API を呼べない。
   プレビュー環境の URL を許可するか（NatuEve_Web_Frontend#330）は未決。ワイルドカードは使えないため、許可する場合は完全一致の値を追加する。
6. **CI/CD**（#219）: GitHub Actions か Cloud Build で、マイグレーションの適用 → Cloud Run へのデプロイを自動化する。
   main へのマージで検証環境へデプロイし、本番へは承認を経て同じイメージを出す（[ADR-0036](./adr/0036-github-flow-staging-approval.md)）。
7. **利用者 IP の取得方式の検証**（#215）: ADR-0034 で、Worker が渡すヘッダーを Gin の `TrustedPlatform` で読む方式に決めた。
   本番では `TRUSTED_PROXIES` を使わない。独自ドメイン経由で利用者 IP が正しく取れ、偽装できないことを検証する。
   参加申込の IP レート制限（`internal/middleware/ratelimit.go`）は `c.ClientIP()` ごとに数えるため、これが正しいことが前提。
8. **停止時の待ち時間**（#216）: 現在の `cmd/api/main.go` は、HTTP サーバーの停止待ち（最大 10 秒）とワーカーの停止待ち（最大 10 秒）を順に行う。
   合計が Cloud Run の猶予（10 秒）を超えうる。
9. **最大インスタンス数**（#215）: 費用の上限と、排他の実装状況を踏まえて決める。
   IP レート制限はインスタンスのメモリ上で数えるため、インスタンスが N 台あると制限は最大 N 倍まで緩む。
   最大インスタンス数で抑えられるのは CPU・メモリの費用で、run.app の URL への直接の大量アクセスによる件数の課金は抑えられない（ADR-0034）。
10. **API の中継の検証**（#226）: 方式は ADR-0034 で決めた（Workers の中継、秘密ヘッダーで中継元を確認）。
    サブドメインは `natueve-api.natuportal.org`。中継用 Worker（`workers/api-relay/`）と API 側の確認・利用者 IP の読み取り
    （`internal/middleware/relay.go`）は実装済み。
    **独自ドメイン経由の検証は未実施**。デプロイ後に[下記の手順](#独自ドメイン経由の検証226)で検証し、結果をここに反映する。
    検証で成り立たなければロードバランサーに切り替える（ADR-0034 の決定事項 7）。
11. **予算アラート**: Cloud Billing の予算アラートを設定する。金額は未決。
    run.app の URL への直接の大量アクセスによる費用の急増を、[下記](#runapp-の-url-へ直接の大量アクセスを受けたとき)の手順の起点として検知する。

## run.app の URL へ直接の大量アクセスを受けたとき

ADR-0034 の構成では、秘密ヘッダーのないリクエストも Cloud Run のコンテナまで届き、件数に応じて課金される。

1. **検知**: 予算アラート、または Cloud Run のリクエスト数と、秘密ヘッダーの確認で拒否した件数の急増。
   拒否したリクエストは、API のログに `中継元の確認に失敗したリクエストを拒否しました`（Warn）として出る。
2. **一時的な対処**: Cloud Run のサービスを別名で作り直し、新しい run.app の URL を Worker の転送先（`wrangler secret put ORIGIN_URL`）に設定してから、
   古いサービスを削除する。Cloud Scheduler の呼び出し先も新しい URL に変える。run.app の URL はサービス名を含み、
   プロジェクト番号は秘密ではないため、新しい名前はランダムな文字列を含む推測されにくいものにする。新しい URL を知られれば効果はない。
3. **恒久的な対処**（ADR-0034 の決定事項 7）: Cloud Run の IAM 認証を追加し、Worker が ID トークンを付けて呼ぶ。
   または外部ロードバランサーに切り替え、Cloud Run の ingress を「内部と Cloud Load Balancing」に制限する。
   いずれも実装・設定の作業が必要で、終えるまでの間は課金が続く。
4. **秘密ヘッダーの値が漏れた疑いがある場合**: 値を差し替える。次の順で行う。
   1. Secret Manager に新しいバージョンを追加し、API の `RELAY_SECRET` を新しいバージョン、`RELAY_SECRET_PREVIOUS` を古いバージョンにしてデプロイする。
   2. Worker を新しい値に切り替える（`wrangler secret put RELAY_SECRET`）。
   3. API から `RELAY_SECRET_PREVIOUS` を外してデプロイし、Secret Manager の古いバージョンを無効化する。

## 独自ドメイン経由の検証（#226）

API と Worker をデプロイした後に行う。結果は #226 に返信し、[未決事項](#未決事項今後の作業) 10 に反映する。
`<run.app>` は Cloud Run の URL、`<フロント>` は `CORS_ALLOW_ORIGINS` に入れたオリジン。

| 確認すること | 手順 | 期待する結果 |
|---|---|---|
| HTTPS で届く | `curl -sS https://natueve-api.natuportal.org/health` | `{"status":"ok"}` |
| 認証付きの API | `curl -sS -H "Authorization: Bearer <JWT>" https://natueve-api.natuportal.org/api/v1/me` | 200 |
| CORS のプリフライト | `curl -sS -i -X OPTIONS -H "Origin: <フロント>" -H "Access-Control-Request-Method: POST" -H "Access-Control-Request-Headers: authorization,content-type" https://natueve-api.natuportal.org/api/v1/events` | 204、`Access-Control-Allow-Origin: <フロント>` |
| run.app への直接アクセス | `curl -sS -i https://<run.app>/api/v1/events` | 403（`/health` は 200） |
| `workers.dev`・Preview URL | ダッシュボードの Worker の「Domains & Routes」で無効になっていること。`https://natueve-api-relay.<アカウントのサブドメイン>.workers.dev/health` | 届かない |
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

- Cloud Run: [billing settings](https://docs.cloud.google.com/run/docs/configuring/billing-settings) / [CPU limits](https://docs.cloud.google.com/run/docs/configuring/services/cpu) / [pricing](https://cloud.google.com/run/pricing)
- Google Cloud: [Free Tier](https://docs.cloud.google.com/free/docs/free-cloud-features) / [Cloud Scheduler pricing](https://cloud.google.com/scheduler/pricing) / [VPC pricing（egress 料金改定の告知）](https://cloud.google.com/vpc/pricing-announce)
- Supabase: [Pricing](https://supabase.com/pricing) / [Regions](https://supabase.com/docs/guides/platform/regions) / [Connecting to Postgres](https://supabase.com/docs/guides/database/connecting-to-postgres) / [Securing your API](https://supabase.com/docs/guides/api/securing-your-api)
- Cloud Run の独自ドメイン: [Mapping custom domains](https://docs.cloud.google.com/run/docs/mapping-custom-domains) / [Cloud Load Balancing pricing](https://cloud.google.com/load-balancing/pricing) / [Ingress](https://docs.cloud.google.com/run/docs/securing/ingress)
- Cloudflare: [R2 pricing](https://developers.cloudflare.com/r2/pricing/) / [Email Service pricing](https://developers.cloudflare.com/email-service/platform/pricing/)
- Cloudflare Workers: [Pricing](https://developers.cloudflare.com/workers/platform/pricing/) / [Custom Domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/) / [workers.dev](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/) / [Protecting your origin server](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/) / [HTTP headers](https://developers.cloudflare.com/fundamentals/reference/http-headers/)
- メール: [Resend pricing](https://resend.com/pricing) / [Amazon SES pricing](https://aws.amazon.com/ses/pricing/)
