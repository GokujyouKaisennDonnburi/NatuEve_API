# ADR-0036: GitHub Flow を続け、main へのマージで検証環境へ、承認後に同じイメージを本番へデプロイする

- ステータス: Accepted
- 日付: 2026-10-23
- 関連: #237, #219, #213, #214, ADR-0003, ADR-0032, ADR-0034, ADR-0035
- 変更: ADR-0032 の「本番公開時は Supabase Pro が前提」を撤回する（本番も Free のまま運用し、バックアップは一旦取らない）
- 被変更: 決定事項 4 の R2（検証用のバケットと、そのバケットだけを操作できるトークン）は [ADR-0039](./0039-r2-production-bucket-split.md) で変更した（検証環境はローカル開発用のバケットを共用し、トークンは検証環境用に別に発行する）。「トレードオフ・制約」の「R2 のバケット名の既定値」は、検証用の専用バケットを前提にした記述

## コンテキストと課題

開発中のブランチ運用は「作業ブランチ → main」の PR だけで、main への直接 push はブロックしている。
デプロイの自動化（#219）は未着手で、#219 の受け入れ基準は「main へのマージでマイグレーションの適用 → Cloud Run へのデプロイ」になっている。
このまま自動化すると、main へのマージがそのまま本番（ADR-0032）に出る。
リリース後は、本番に出す前に、本番と同じ構成で結合しても問題ないかを確かめたい。

決める必要があったのは次の 3 点。

1. ブランチ運用（main の前に dev などのブランチを挟むか）。
2. 本番に出す前に確かめる場所と、本番へ出す操作。
3. 検証環境の DB・認証の置き場所と、本番 DB のバックアップ。

前提となる制約（2026-10 時点で公式ドキュメントを確認）:

- Supabase は Pro にしない。本番も Free のまま運用する。
- Supabase のプランは組織（Organization）ごとに決まる。
  Free の「アクティブなプロジェクトは 2 つまで」は組織ごとではなくユーザーごとに数え、
  そのユーザーが Owner または Administrator になっている全組織を合算する（一時停止中のプロジェクトは数えない）。
- なちゅいべ Org（Free）にはプロジェクトが既に 2 つある。
- Supabase Free には自動バックアップがない。
- リポジトリは public で、GitHub の Organization は Free プラン。
  GitHub の Environments は Free プランの public リポジトリで使える（private リポジトリで使うには、Organization では GitHub Team が要る）。

## 決定

**ブランチは GitHub Flow（作業ブランチ → main）のまま増やさない。main へのマージで検証環境へデプロイし、
本番へは承認を経て、検証環境に出したのと同じコンテナイメージを出す。**

1. **ブランチ**: main ← 作業ブランチ の PR だけで運用する。dev などの長期ブランチは作らない。
   本番の緊急修正も同じ経路（main → 検証環境 → 承認 → 本番）を通る。
2. **検証環境へのデプロイ**: main へのマージで、コンテナイメージを 1 回だけビルドして Artifact Registry に置き、
   検証用 DB へのマイグレーションの適用 → 検証環境の Cloud Run へのデプロイ を自動で行う。
3. **本番へのデプロイ**: GitHub Environments の `production` に承認者（Required reviewers）を設定し、承認されてから行う。
   - 本番に出すのは、検証環境に出したイメージをダイジェストで指定したもの。本番用にビルドし直さない。
   - 承認後、本番 DB へのマイグレーションの適用 → 本番の Cloud Run へのデプロイ の順に行う。
   - 本番用の認証情報は `production` 環境に置き、承認を通ったジョブだけが使えるようにする。GCP への認証方式は #219 で決める。
   - ワークフローの形（同じ実行の中で承認待ちにするか、タグの push を起点にするか）、
     承認待ちが複数溜まったときに古いイメージを出さない仕組み、
     自分が起動した実行を自分で承認できなくする設定（Prevent self-review）を使うかは #219 で決める。
4. **検証環境の構成**: 本番と同じ構成にし、接続先は環境変数で切り替える。環境ごとのコードの分岐は作らない。
   分けるものは次のとおり。

   | 対象 | 検証環境で用意するもの | 環境変数 |
   |---|---|---|
   | Cloud Run | 検証用のサービス | — |
   | Supabase（DB・Auth） | 検証用のプロジェクト（決定事項 5） | `DATABASE_URL`, `SUPABASE_JWKS_URL` |
   | Cloudflare R2 | 検証用のバケットと、そのバケットだけを操作できるトークン | `R2_BUCKET`, `R2_PUBLIC_BASE_URL`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` |
   | Resend | 検証用の送信専用 API キー（ADR-0003） | `RESEND_API_KEY`, `MAIL_FROM` |
   | 中継用 Worker（ADR-0034） | 検証用の Worker・独自ドメインと、別の秘密値 | `RELAY_SECRET` |
   | Cloud Scheduler（ADR-0035） | 検証環境の URL を呼ぶジョブと、別のサービスアカウント | `SCHEDULER_OIDC_AUDIENCE`, `SCHEDULER_SERVICE_ACCOUNT_EMAIL` |
   | Secret Manager | 検証用のシークレット | — |
   | CORS | フロントの検証環境のオリジン | `CORS_ALLOW_ORIGINS` |

   - 共有するもの: Cloudflare のアカウント（R2・Workers）、Resend のアカウント、GCP の請求アカウント、GitHub のリポジトリ。
   - 未決（#219 で決める）: GCP のプロジェクトを本番と分けるか、Resend の送信元を検証用のサブドメインにするか、検証用 Worker のドメイン名。
5. **検証用の Supabase**: なちゅいべ Org とは別の Supabase アカウント（NatuPortal の共有 Auth と同じアカウント）に、
   Free の検証用の組織とプロジェクトを作る。
   - 検証用の DB と Auth は 1 つのプロジェクトにまとめる。
     検証環境の認証には検証用プロジェクトの Auth を使い、本番の Auth（NatuPortal の共有 Auth）は使わない。
   - 検証用 DB には本番のデータをコピーせず、テスト用のデータを使う。
6. **バックアップ**: Supabase は本番・検証とも Free のまま（Pro にしない）。本番 DB のバックアップは一旦取らない。

## 理由

- **ブランチを増やさない**: 本番に出す前の確認に必要なのは、本番と同じ構成の検証環境である。
  ブランチはどの時点のコードを載せるかの目印にすぎず、main の内容を検証環境に載せれば確認できる。
  dev を挟むと次の負担が増える。
  - 本番の緊急修正を main に直接入れたとき、dev へ戻し忘れると次のリリースで修正が消える。
  - リリースが「dev に溜まった変更すべて」の単位になり、1 つの変更に問題があると他の変更も止まる。
  - dev → main の PR は中身がレビュー済みで、確認が形だけになりやすい。
    squash マージすると履歴が分かれ、以降の dev → main で毎回衝突する。
- **同じイメージを出す**: 検証環境で確かめたものと本番に出すものが一致する。
  ビルドし直すと、依存やベースイメージの取得のし直しで中身が変わりうる。
- **承認に GitHub Environments を使う**: public リポジトリのため Free プランで使え、追加のサービスや費用が要らない。
  承認者と承認の記録が GitHub に残り、承認を通ったジョブだけが本番用の認証情報を使える。
- **検証用 Supabase を別アカウントの組織に置く**: なちゅいべ Org の Owner / Administrator の Free の枠（2 つ）は埋まっている。
  Free の枠はユーザーごとに数えるため、別のアカウントの組織なら費用 $0 で作れる。
  Supabase を使うので、本番と同じ PostgreSQL 17・`pg_trgm`・接続方式（#213）で確かめられる。
- **検証用の Auth を分ける**: 本番の共有 Auth を使うと、テスト用アカウントが本番の Auth に入り、NatuPortal の他のプロダクトにもログインできてしまう。
  また、本番の利用者のトークンで検証用 API を使えてしまう。
- **本番のデータをコピーしない**: 本番の DB には希少種の生息地の座標と個人情報がある。
  検証環境に複製すると、座標保護（CLAUDE.md「座標情報の保護」）の対象が、守りの弱い環境に広がる。
- **Pro にしない**: 費用を抑えるため。Pro は月 $25（日次バックアップ 7 日分を含む）で、別プロジェクトを足すと Micro で月 約 $10 が加わる。
  Free には自動バックアップがなく、本番 DB のバックアップは一旦取らない（自前で取る案は「検討した代替案」を参照）。

## 影響（結果）

### 良い影響
- 長期ブランチがないため、main と別ブランチのずれが起きない。issue と PR は 1 対 1 のまま。
- マイグレーションは本番より先に検証用 DB に適用され、失敗が本番に届く前に見つかる。
- 本番で問題が出たときは、Cloud Run の前のリビジョンにトラフィックを戻せる（DB のマイグレーションは戻らない）。
- Supabase の費用は本番・検証とも $0 のまま。

### トレードオフ・制約
- **main の意味が変わる**: main は「本番に出ている内容」ではなく「次に本番へ出す候補」になる。
  本番に出ている内容は、承認されたデプロイの記録で確かめる。
- **緊急修正も承認を待つ**: 承認者が誰も応じられないと、本番の緊急修正も出せない。
  承認者は最大 6 人（ユーザーまたはチーム）まで登録でき、承認は 1 人で足りるため、複数人を登録しておく。
  承認待ちは最大 30 日で、それを過ぎると失敗になる。
- **マイグレーションは旧リビジョンでも動く形にする必要がある**: 本番ではマイグレーションの適用からデプロイが終わるまで旧リビジョンが動く。
  前のリビジョンへ戻すときも DB は新しいままになる。
- **本番 DB を戻せない**: バックアップがないため、障害・誤操作・誤ったマイグレーションでデータを失うと戻せない。
- **Supabase Free の制限（本番・検証とも）**:
  - DB は 500MB まで。
  - 過去 7 日間の DB の利用が少ないと一時停止する。どちらの環境も Scheduler が毎分 DB を問い合わせるため避けられる見込みだが、公式の定義は曖昧で保証はない。
- **検証用 Supabase の枠**: 検証用の組織を持つアカウントには NatuPortal の共有 Auth のプロジェクトがあるため、
  検証用を足すとアクティブなプロジェクトは 2 つになる（他にアクティブなプロジェクトがない場合）。
  そのアカウントでは、Owner / Administrator になっている他の組織も含めて、これ以上アクティブなプロジェクトを足せない。
- **検証環境と本番の構成の差**: 検証用は DB と Auth が同じプロジェクトにある。
  本番で DB を Auth と別のプロジェクトに置く場合（#214）、Data API を塞ぐ設定の確認は検証環境と本番で別になる。
- **R2 のバケット名の既定値**: `R2_BUCKET` は未設定だと `natuportal` になる。
  検証環境では必ず設定し、トークンを検証用バケットに限ることで、設定漏れが本番バケットへの書き込みにならないようにする。
- **無料枠を本番と分け合う**:
  - Cloud Scheduler の無料枠（請求アカウントあたり 3 ジョブ）のうち、本番と検証の outbox で 2 つを使う。
  - Secret Manager は本番だけで無料枠（アクティブなバージョン 6 つ）をほぼ使うため、検証用のシークレットは少額の課金になる。
  - Cloud Run の無料枠は請求アカウント単位で、本番と検証で分け合う。
- **フロントの検証環境**: フロントとの結合を確かめるには、フロント側の検証環境が要る（NatuEve_Web_Frontend 側で決める）。
  検証用 API の `CORS_ALLOW_ORIGINS` にはそのオリジンを入れる。

## 検討した代替案

- **main ← dev ← 作業ブランチ（dev を検証環境、main を本番に対応させる）**: 「main ＝ 本番」が分かりやすく、決まった日にまとめて出す運用に向く。
  ただし「理由」に書いたずれ・一括リリース・PR が形だけになる負担があり、承認付きで同じイメージを出せば同じ確認ができる。不採用。
- **Cloud Run のトラフィック 0 のリビジョンで、本番上で確かめる（検証環境を作らない）**: `gcloud run deploy --no-traffic --tag` で本番のサービスに載せ、
  タグ付きの URL で確かめてから切り替える。本番の設定のまま確かめられるが、DB は本番と共有のため、マイグレーションは確かめる前に本番に適用される。
  run.app の URL を直接呼ぶには中継の秘密ヘッダー（ADR-0034）も要る。単独では不採用（検証環境と併用することは妨げない）。
- **Cloudflare D1（本番・検証の DB）**: Workers Paid で使え、DB を多数作れる（アカウントあたり 50,000 個）。次の理由で使えない。不採用。
  - Cloud Run の Go から DB として接続できない。外から使えるのは REST API だけで、公式には主に DB の作成・管理用とされている。
    リポジトリ層（`database/sql` + pgx）を HTTP 呼び出しに書き換える必要がある。
  - REST API は複数の文をまとめて実行する形で、途中に Go の処理を挟むトランザクションを組めない。
  - SQLite のため、マイグレーション（goose の PostgreSQL 方言）とクエリが使っている PostgreSQL 固有の機能がない。
    `pg_trgm` の GIN インデックス（ADR-0030）、`normalize()` による NFKC 正規化と `ILIKE`（ADR-0008 のイベント検索）、
    `FOR UPDATE SKIP LOCKED`（ADR-0035）、`gen_random_uuid()`、`timestamptz`。
  - D1 を前提にするなら、API を Workers（TypeScript）で書き直すことになる。
  - 検証環境だけを D1 にすると、本番と DB の種類が違い、検証環境で確かめても本番で通る保証にならない。
- **Cloudflare のその他（Hyperdrive・Durable Objects・Containers）**: Hyperdrive は Workers から既存の Postgres への接続を速くする仕組みで、DB ではない。
  Durable Objects の SQLite は外から接続できない。Containers はディスクが一時的でスリープすると消え、外からの TCP 接続を受けられない。不採用。
- **PlanetScale Postgres（Cloudflare の請求にまとめられる）**: PostgreSQL 17・`pg_trgm`・東京リージョンがあるが、最小でも月 $5（us-east-1 の価格）かかり、
  検証用の Auth は別に要る。費用を抑えるため不採用。
- **Neon Free（検証用 DB）**: 無料で PostgreSQL 17・`pg_trgm` が使える。ただし計算時間は月 100 CU 時間で、毎分の Scheduler で DB が休止しないと、
  最小の 0.25 CU でも月 約 182 CU 時間かかり、月の途中で止まる。東京リージョンがなく、
  Supabase と接続方式が違うため Supabase 固有の制約（#213）を確かめられない。検証用の Auth も別に要る。不採用。
- **検証用プロジェクトをなちゅいべ Org に足す**: Free の枠（2 つ）が埋まっていて作れない。不採用。
- **Supabase Pro にして同じ組織に足す**: 本番と同じ組織で管理でき、日次バックアップも付くが、月 $25 に加えて検証用プロジェクトに月 約 $10 かかる。
  費用を抑えるため不採用。
- **Supabase の Branching**: 使った時間で課金され、Pro のクレジットは使えない。料金ページでは Free に含まれないと読める。Pro にしないため不採用。
- **本番の Supabase プロジェクトの中にスキーマを分けて検証環境を置く**: 枠も費用も増えないが、Free の 500MB を分け合い、
  検証のつもりの操作やマイグレーションの誤りが本番 DB に当たる。不採用。
- **検証環境の認証に本番の共有 Auth を使う**: 検証用プロジェクトの Auth が要らなくなるが、「理由」に書いた問題がある。不採用。
- **本番 DB のバックアップを自前で取る（`pg_dump` を定期実行して R2 に置く）**: 金額はほぼかからない
  （Free の DB は最大 500MB で R2 の無料枠に収まる。Cloud Scheduler で起動するなら 3 ジョブ目になる）。
  取る場合、ダンプには希少種の座標と個人情報が入るため、専用の非公開バケット・トークンの限定・暗号化が要る。
  決定事項 6 のとおり一旦取らず、取るかどうかは改めて決める。

## 参考（2026-10 時点）

- Supabase: [Billing on Supabase](https://supabase.com/docs/guides/platform/billing-on-supabase)（Free の枠の数え方） /
  [Free project pausing](https://supabase.com/docs/guides/platform/free-project-pausing) / [Pricing](https://supabase.com/pricing)
- Cloudflare D1: [Query D1](https://developers.cloudflare.com/d1/best-practices/query-d1/) / [Limits](https://developers.cloudflare.com/d1/platform/limits/)
- Cloudflare: [Storage options](https://developers.cloudflare.com/workers/platform/storage-options/) /
  [Containers platform details](https://developers.cloudflare.com/containers/platform-details/)
- PlanetScale: [Pricing](https://planetscale.com/pricing) /
  [PlanetScale Postgres と Workers（Cloudflare の発表）](https://blog.cloudflare.com/planetscale-postgres-workers/)
- Neon: [Plans](https://neon.com/docs/introduction/plans)
- GitHub: [Managing environments for deployment](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments) /
  [Reviewing deployments](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/review-deployments)
