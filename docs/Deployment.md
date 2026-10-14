# デプロイ構成（バックエンド）

NatuEve_API の本番デプロイ構成をまとめる。
構成を選んだ理由と退けた代替案は [ADR-0032](./adr/0032-production-deployment-cloud-run-supabase.md) を参照。
料金・仕様は 2026-10 時点で各公式ページを確認した値（出典は末尾）。

## 使用するサービス

| サービス | 用途 | 状態 |
|---|---|---|
| **Google Cloud Run**（`asia-northeast1`） | API コンテナの実行 | 確定 |
| **Google Cloud Scheduler** | outbox 処理エンドポイントの定期呼び出し（1 分間隔） | 確定 |
| **Supabase**（`ap-northeast-1`） | PostgreSQL 17（DB） | 確定 |
| **Supabase Auth** | 認証（JWT 発行・JWKS による検証） | 確定（既存） |
| Cloudflare R2 | 画像・PDF の保存と配信 | 既存 |
| Resend | トランザクションメールの送信（[ADR-0001](./adr/0001-resend-transactional-email.md)） | 既存 |
| Google Artifact Registry | コンテナイメージの保管 | 予定 |
| Google Secret Manager | 秘密情報の注入 | 予定 |

## 構成図

```
[Web フロント / モバイルアプリ]
   │
   ├─ ログイン ─────────────→ Supabase Auth（JWT 発行）
   │
   └─ API 呼び出し（JWT 付き）─→ Cloud Run: NatuEve_API ←── Cloud Scheduler（1 分ごと、outbox 処理）
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
| ヘルスチェック | `GET /health` |
| イメージ | リポジトリ直下の `Dockerfile`（distroless・`./cmd/api`） |

- リクエスト課金では、レスポンスを返した後の goroutine に CPU が割り当てられない。
  バックグラウンド処理を API プロセス内に置かない。
- 1 vCPU 未満にする場合は、同時実行数 1・第 1 世代実行環境が条件になる。
- 停止時は SIGTERM を受けてから 10 秒の猶予がある。

### Cloud Scheduler

| 項目 | 値 |
|---|---|
| 呼び出し間隔 | 1 分 |
| 呼び出し先 | outbox を 1 回処理する内部エンドポイント（未実装） |
| 認証 | Scheduler 以外から呼べない方式にする（方式は未決） |

- 無料枠は請求アカウントあたり 3 ジョブ。ジョブを一時停止しても課金対象として数えられる。

### Supabase

| 項目 | 値 |
|---|---|
| リージョン | `ap-northeast-1`（東京） |
| プラン | 公開前は Free、本番公開時に Pro |
| Data API（PostgREST） | NatuEve のテーブルに到達できない状態にする（無効化、または API 用ロールへ権限を付与しない） |
| Storage | 使わない（ファイルは Cloudflare R2） |

- Free プランの制限: DB 500MB、自動バックアップなし、1 週間アクティビティがないと一時停止、アクティブなプロジェクトは 2 つまで。
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
| `TRUSTED_PROXIES` | 未決（Cloud Run の前段での値を要検証） | |
| `CORS_ALLOW_ORIGINS` | 本番のフロントのオリジン（未決。フロント側 NatuEve_Web_Frontend#328 で決まる） | |
| `R2_ACCOUNT_ID` | Cloudflare のアカウント ID | |
| `R2_ACCESS_KEY_ID` | R2 の API トークン | ○ |
| `R2_SECRET_ACCESS_KEY` | R2 の API トークン | ○ |
| `R2_BUCKET` | バケット名 | |
| `R2_PUBLIC_BASE_URL` | 公開配信のベース URL | |
| `RESEND_API_KEY` | Resend の API キー | ○ |
| `MAIL_FROM` | 送信元アドレス | |

- Secret Manager の無料枠は、アクティブなシークレットバージョン 6 つ・アクセス 1 万回／月。
  秘密情報は 4 つなので無料枠に収まる。

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
| Cloudflare R2 | 0 | 約 0.3 | 約 4.7 |
| Resend | 0（Free） | 20（Pro） | 35（Pro） |
| Supabase | 0（Free）／25（Pro） | 25 | 25〜 |
| **合計** | **約 0〜25** | **約 46** | **約 94〜** |

試算の前提:

- API は 1 リクエストあたり処理 0.1 秒・応答 5KB、1 vCPU・512MiB。同時に処理したリクエストの時間を重ねずに足しているため、Cloud Run は多めの見積もり。
- 1 PV あたり画像を 5 枚読み込む。Scheduler は 1 分ごと（月 約 4.3 万回）。
- Cloud Run の無料枠（請求アカウント単位、リクエスト課金）: 月 200 万リクエスト・18 万 vCPU 秒・36 万 GiB 秒。
- Cloud Run の超過単価は第三者サイトの us-central1 の値（vCPU 秒 $0.000024・GiB 秒 $0.0000025・100 万リクエスト $0.40）。東京の単価は未確認。
- 外への通信は、アジア向けの単価 $0.12/GiB（2024 年の料金改定告知の値）で計算した。
- 大規模で Supabase のコンピュート増強が必要になるかは未確認。

## 未決事項・今後の作業

1. **outbox の Scheduler 起動の実装**（ADR-0032 の決定事項 2、#217）
   - 内部エンドポイントの認証方式、排他方式（`FOR UPDATE SKIP LOCKED` またはアドバイザリロック）、
     ローカル開発での起動方法、`Wake()` の削除。
   - **これが完了するまで、本構成では通知メールの送信が保証されない。** 本番公開の前提条件。
     現在の常駐ワーカーは、リクエスト処理中にしか CPU を得られない。インスタンスが 0 台になると止まり、
     複数インスタンスでは二重送信の恐れがある。
   - 実装前にデプロイする場合の暫定策の候補:
     - 最大インスタンス数を 1 にする（二重送信を防ぐ。送信タイミングは保証されない）
     - `RESEND_API_KEY` を設定せず、ワーカーを起動させない。予約は outbox に溜まり、設定後に送信される（ADR-0016 決定事項 4 の挙動）
2. **Supabase への接続方式**（#213）
   - 直接接続は IPv6 のみ（IPv4 は有料アドオン）。
   - セッションモードのプーラー（ポート 5432）は IPv4 で使える。
   - トランザクションモードのプーラー（ポート 6543）は prepared statement に対応していない。
   - pgx（`pgx/stdlib` 経由の `database/sql`）の既定動作との組み合わせを検証して決める。
3. **Supabase で PostgreSQL 17 と `pg_trgm` が使えるかの確認**（ADR-0030 のインデックスが前提、#213）。
4. **DB を Auth と同じ Supabase プロジェクトに置くか、別プロジェクトにするか**（#214）
   - Auth は NatuPortal の複数プロダクトで共有しているため、Data API を無効化すると他プロダクトに影響しうる。
   - 別プロジェクトにすると、Pro でもコンピュート費用（Micro で月 $10）が追加される。
5. **CORS の許可オリジンの値**: 環境変数 `CORS_ALLOW_ORIGINS` で設定できる（#218、ADR-0033）。
   本番の値は、フロントのオリジン（NatuEve_Web_Frontend#328）が決まり次第設定する。
   未設定のままだと開発用の `http://localhost:3000`・`http://localhost:8787` だけが許可され、本番のフロントから API を呼べない。
   プレビュー環境の URL を許可するか（NatuEve_Web_Frontend#330）は未決。ワイルドカードは使えないため、許可する場合は完全一致の値を追加する。
6. **CI/CD**（#219）: GitHub Actions か Cloud Build で、マイグレーションの適用 → Cloud Run へのデプロイを自動化する。
7. **`TRUSTED_PROXIES` の値**（#215）: Cloud Run の前段から見たクライアント IP の扱いを検証して決める。
   参加申込の IP レート制限（`internal/middleware/ratelimit.go`）は `c.ClientIP()` ごとに数えるため、この値が正しいことが前提。
8. **停止時の待ち時間**（#216）: 現在の `cmd/api/main.go` は、HTTP サーバーの停止待ち（最大 10 秒）とワーカーの停止待ち（最大 10 秒）を順に行う。
   合計が Cloud Run の猶予（10 秒）を超えうる。
9. **最大インスタンス数**（#215）: 費用の上限と、排他の実装状況を踏まえて決める。
   IP レート制限はインスタンスのメモリ上で数えるため、インスタンスが N 台あると制限は最大 N 倍まで緩む。
10. **API の独自ドメイン**（#221）: Cloud Run への割り当て方法を決める。公式推奨のロードバランサーは転送ルールだけで約 $18/月かかり、
    上の月額試算には含まれていない。無料の domain mapping は Preview 段階で、本番には非推奨。

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
- Cloudflare: [R2 pricing](https://developers.cloudflare.com/r2/pricing/) / [Email Service pricing](https://developers.cloudflare.com/email-service/platform/pricing/)
- メール: [Resend pricing](https://resend.com/pricing) / [Amazon SES pricing](https://aws.amazon.com/ses/pricing/)
