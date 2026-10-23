# ADR-0034: API の独自ドメインを Cloudflare Workers の中継で Cloud Run に割り当て、中継元を秘密ヘッダーで確かめる

- ステータス: Accepted
- 日付: 2026-10-18
- 関連: #221, #226, #215, #217, #219, ADR-0032, NatuEve_Web_Frontend ADR-0001
- 構成の詳細・設定値は [docs/Deployment.md](../Deployment.md) を参照
- 注記: 最大インスタンス数は #240 で扱い、[ADR-0037](./0037-cloud-run-max-instances.md) で決めた（サービス単位で 1）

## コンテキストと課題

ADR-0032 で API の実行基盤を Cloud Run に決めた。Cloud Run はサービスごとに、サービス名を含む
`run.app` ドメインの URL（以下 run.app の URL）を発行する。
クライアントに見える API の URL を、自分たちのドメインに固定する方法が未決だった。

判断に効く前提は次のとおり（料金・仕様は 2026-10 時点で公式ドキュメントを確認した値）。

- クライアントは Web フロント（ブラウザ）・モバイルアプリ・フロントの SSR の 3 つ。モバイルアプリは API の URL を
  埋め込むため、後から URL を変えるとアプリの更新が必要になる。
- Cloud Run の公式推奨は外部アプリケーションロードバランサーで、転送ルールだけで月 約 $18 かかる。
  公開初期の月額試算（約 $0〜25、Deployment.md）に対して大きい。無料の domain mapping は Preview で、
  公式に「not production-ready」とされている。
- フロントは Cloudflare Workers の Paid プラン（月 $5）で動かすことに決まっている（NatuEve_Web_Frontend ADR-0001）。
  ドメイン（`natuportal.org`）の DNS と R2 も Cloudflare で管理している。フロントの SSR（Worker）からも API を呼ぶ。
- 前段に何を置いても run.app の URL は残り、インターネットから直接呼べる。前段の防御（DDoS 対策・WAF）を迂回されうる。
- 参加申込の IP レート制限（`internal/middleware/ratelimit.go`）は `c.ClientIP()` ごとに数える。前段に中継を挟むと、
  利用者 IP の受け渡し方と、その偽装への対策が必要になる。

## 決定

**API の独自ドメイン（`api.〈ドメイン〉`）を Cloudflare Workers の Custom Domain に割り当て、中継用の Worker から
run.app の URL へ転送する。run.app の URL への直接アクセスは、Worker だけが付ける秘密ヘッダーを API が確かめて拒否する。**

1. **ドメイン**: API は独自ドメインのサブドメインに置く（名前は未決。例: `api.natuportal.org`）。
   フロントと同じオリジンのパス（`/api/*`）には置かない。
2. **中継**: サブドメインを中継用 Worker の Custom Domain に割り当てる（Routes は使わない）。
   Worker は転送先を run.app の固定の URL とし、リクエストからはパスとクエリだけを引き継ぐ。
3. **中継元の確認**: Worker は転送のたびに秘密ヘッダー（ランダムな値）を付ける。API は全リクエストでこの値を確かめ、
   一致しなければ拒否する。確認から外す経路（`/health`、Cloud Scheduler から呼ぶ内部エンドポイントなど）は実装時に決める。
4. **利用者 IP**: Worker は Cloudflare が付けた `CF-Connecting-IP` の値を、利用者 IP 用のヘッダーに上書きして渡す。
   API は Gin の `TrustedPlatform` でそのヘッダーを読む。本番では `TRUSTED_PROXIES` を使わない。
5. **Worker のコードの置き場所**: 本リポジトリに置く。
6. **Cloud Run の公開設定**: 認証なし（`allUsers` に invoker を付ける）のままにし、IAM 認証は使わない。
7. **切り替え条件**: 次の場合は構成を切り替える。いずれもクライアントから見た URL は変わらない。
   - 検証で Worker 経由の構成が成り立たない（Cloud Run が転送を受け付けない、利用者 IP を正しく取れない、
     遅延が許容できない等）場合は、外部アプリケーションロードバランサーに切り替える。
   - run.app の URL へ直接の大量アクセスを受けた場合は、Cloud Run の IAM 認証を追加し、Worker が ID トークンを付けて呼ぶ。

実装と検証は #226 で行う。利用者 IP の検証と最大インスタンス数の決定は #215 で行う。

**#226 の実装時に決めた事項**:

- 決定事項 1 のサブドメインは `natueve-api.natuportal.org` とする。`natuportal.org` は NatuPortal の複数プロダクトで共有するため、プロダクト名を含める。
- 決定事項 3 で確認から外す経路は `/health`（Cloud Run のヘルスチェックが秘密ヘッダーなしで呼ぶ）と
  `/internal/outbox/process`（Cloud Scheduler が run.app の URL を直接呼び、OIDC トークンで認証する。ADR-0035）とし、パスの完全一致で判定する。
  確認に通らないリクエストでは、確認から外す経路でも利用者 IP のヘッダーを使わない。
- 転送先の run.app の URL と秘密値は Worker の secret にし、リポジトリに書かない。本リポジトリは公開されており、run.app の URL を
  推測されにくくしておくことが、直接の大量アクセスを受けたときの一時的な対処（docs/Deployment.md）の前提になるため。
- 秘密値と利用者 IP のヘッダー名の片方だけが設定されている場合、API は起動しない。両方未設定なら確認を行わずに起動する（ローカル開発）。
- Gin の末尾スラッシュ違いのリダイレクト（`RedirectTrailingSlash`）は無効にし、末尾スラッシュ違いのパスは 404 にする。
  このリダイレクトは middleware より前に応答するため、有効のままだと秘密ヘッダーなしでも 403 にならない。
  確認を有効にしたときだけ無効にすると、開発環境と本番で応答が変わるため、常に無効にする。

## 理由

- **費用**: 中継は、フロントと共有する Workers Paid の枠（月 1,000 万リクエスト・CPU 3,000 万ミリ秒）に含まれる。
  `fetch()` の待ち時間は CPU 時間に数えられない。中規模（月 100 万リクエスト）まで追加費用は $0 の見込み。
  ロードバランサーの月 約 $18 がかからない。
- **URL の固定**: クライアントの URL を自分たちのドメインに固定できる。裏の中継方式を後で替えても
  （Worker からロードバランサーへ等）、クライアントの URL は変わらない。
- **Cloudflare の防御を前に置ける**: DDoS 対策・WAF を API の前段に置ける。DNS・フロント・R2 と同じ Cloudflare で管理できる。
- **サブドメインにする理由**: 同じオリジンのパスにすると CORS のプリフライトは不要になる。ただし、既存の CORS 設定は
  プリフライトの結果を 12 時間キャッシュさせており（`internal/middleware/cors.go`）、削減できる量は小さい。
  一方で API の URL がフロントのホスト名に縛られ、フロントのホスト名を変えるとモバイルアプリが影響を受ける。
- **Custom Domain にする理由**: 同じゾーンの別の Worker（フロントの SSR）から `fetch()` で呼べるのは Custom Domain の
  Worker で、Routes の Worker は Service Binding が必要になる（Cloudflare 公式）。Custom Domain ならフロントの改修が要らない。
- **中継元を秘密ヘッダーで確かめる理由**:
  - Workers の送信元 IP は公開されておらず、Cloudflare の全利用者で共有される。送信元 IP では自分たちの Worker を見分けられない。
  - Cloud Run 単体でクライアント証明書（mTLS）を検証する方法は、公式ドキュメントで確認できなかった。
  - Cloudflare Tunnel はオリジン側から常時接続を張る仕組みで、Cloud Run で使う公式の手順はない。リクエスト課金では
    リクエストの処理中しか CPU が割り当てられないため、最小 0 台のリクエスト課金（ADR-0032）とは両立しない見込み。
  - 秘密ヘッダーは Cloudflare が全プランで使える方式として挙げており、AWS（CloudFront → ALB）も同じ方式の手順を公式に案内している。
- **IAM 認証を使わない理由**: Worker から ID トークンを得るには、サービスアカウントの鍵を Worker に置く必要がある
  （Google は鍵の持ち出しをセキュリティリスクとして警告している）。Worker に鍵での署名・トークンの取得と更新の処理が加わり、
  Google のトークン発行が全リクエストの経路に入る。秘密ヘッダーが守る対象（次項）に対して、手間と新たな障害点が見合わない。
  後から追加してもクライアントに影響しない。
- **秘密ヘッダーが守るもの**: Cloudflare の防御を迂回されないこと、利用者 IP の正しさ（IP レート制限）、
  直接アクセスによる費用の増加。データの保護（認証・座標保護）は API の JWT 検証と geofuzzing が担っており、
  秘密ヘッダーには依存しない。
- **`TrustedPlatform` にする理由**: `TRUSTED_PROXIES` は送信元 IP で中継を信頼する仕組みで、上記のとおり送信元 IP では
  Worker を見分けられない。`TrustedPlatform` は指定したヘッダーの値を検証せずに返す（Gin v1.12.0 `context.go`）ため、
  決定事項 3 の確認が前提になる。
- **Worker のコードを本リポジトリに置く理由**: 秘密ヘッダーと利用者 IP のヘッダーは Worker と API の間の取り決めで、
  片方だけ変えると壊れる。同じリポジトリなら両方を 1 つの PR で変更・レビューできる。

## 影響（結果）

### 良い影響

- 中規模まで追加費用なしで、API の URL を自分たちのドメインに固定できる。
- 後でロードバランサーへ切り替えても、クライアントの URL は変わらない。
- Cloudflare の DDoS 対策・WAF を API の前段に置ける。
- 中継元の確認をアプリで行うため、run.app 以外の Cloud Run の URL（custom URL 等）から届くリクエストにも効く。

### トレードオフ・制約

- **防御が一段**: 秘密ヘッダーが漏れると、差し替えるまで run.app の URL への直接アクセス（利用者 IP の偽装を含む）が通る。
  Cloudflare は秘密ヘッダーを「Moderately secure」と評価している。AWS・Azure の公式手順にある、送信元 IP の範囲での
  制限との組み合わせはできない。
- **直接の大量アクセスは課金される**: 秘密ヘッダーのないリクエストもコンテナまで届き、件数に応じて課金される。
  最大インスタンス数（#215）で抑えられるのは CPU・メモリの費用で、件数の課金は抑えられない。予算アラートで検知し、
  決定事項 7 の切り替えで対処する。切り替えを終えるまでの間は課金が続く。
- **中継が 1 段増える**: 遅延は未計測。Cloudflare の障害時は API にも届かなくなる（フロントはすでに Cloudflare に依存している）。
- **実装・設定で守る事項**（実装 issue のチェックリストにする）:
  - `workers.dev` と Preview URL を無効にする（Wrangler の `workers_dev = false`・`preview_urls = false`）。
    ダッシュボードで無効にしただけでは、次のデプロイで有効に戻る。
  - Worker は利用者 IP のヘッダーを必ず上書きする。Cloudflare は独自名のヘッダーをオリジンへそのまま渡すため、
    受信したリクエストのヘッダーを流用すると利用者が値を偽装できる。
  - `fetch()` は `redirect: "manual"` にする。`"follow"` では秘密ヘッダーを含む全ヘッダーがリダイレクト先にも送られる。
  - `fetch()` は `cache: "no-store"` にし、API のホスト名にキャッシュルール（「すべてキャッシュ」等）を付けない。
  - 転送先はリクエストの内容から決めず、run.app の固定の URL にする。
  - 秘密ヘッダーの値は 32 バイト以上のランダム値にする。新旧 2 つの値を受け付けられるようにして差し替える。
    比較は定数時間で行い、ログに出さない。
  - Cloudflare の Pseudo IPv4 は既定の Off のままにする。Overwrite にすると IPv6 の利用者 IP が疑似 IPv4 に置き換わり、
    IP レート制限の `/64` 単位の集計が崩れる。
  - 拒否したリクエストのログに、偽装されたヘッダーの IP を記録しない。
  - 確認から外す経路には別の認証が必要になる（Cloud Scheduler の内部エンドポイントは #217）。
- **未検証の前提**（実装 issue・#215 で確かめる）:
  - Cloud Run が、Worker がホスト名を書き換えて転送したリクエストを受け付けるか。
  - 利用者が自分で付けた `CF-Connecting-IP` を Cloudflare が上書きするか（公式に明記がない）。
  - フロントの SSR から Custom Domain の Worker へ `fetch()` できるか（公式には可能と記載）。
  - ゾーンの WAF・レート制限ルールが Custom Domain の Worker 宛てのリクエストに効くか。
  - 中継による遅延。
- **管理するものが増える**: 本リポジトリに Worker のコード（TypeScript）と Wrangler の設定が加わる。
  Worker のデプロイを CI/CD（#219）に含める必要がある。

## 検討した代替案

- **外部アプリケーションロードバランサー＋サーバーレス NEG**: Cloud Run の公式推奨。ingress 設定で run.app の URL への
  直接アクセスを Google 側で止められる。転送ルールだけで月 約 $18 かかり、公開初期の費用に対して大きい。不採用
  （決定事項 7 の切り替え先）。
- **Cloud Run domain mapping**: 無料だが Preview で、公式に本番非推奨（遅延の問題）。不採用。
- **Firebase Hosting の rewrites で中継**: 小〜中規模は無料。タイムアウトが 60 秒、`__session` 以外の Cookie を除去する、
  利用者 IP のヘッダーに公式の保証がない、Firebase という製品が加わる。不採用。
- **Cloudflare の Origin Rules で Worker なしに中継**: Host ヘッダーと SNI の上書きは Enterprise プランのみ。不採用。
- **Cloud Run の custom URL（2026-09 に Preview）**: `*.cloud.run` のサブドメインで、自分たちのドメインにはならないとみられる
  （詳細は未確認）。不採用。
- **API Gateway**: 独自ドメインは Preview で、ロードバランサーが必要。不採用。
- **独自ドメインを使わず run.app の URL を直接使う**: 費用も手間もかからないが、クライアント（特にモバイルアプリ）に
  埋め込んだ URL が実行基盤に縛られる。不採用。
- **フロントと同じオリジンのパス（`/api/*`）に置く**: 上記「サブドメインにする理由」のとおり。Routes の Worker になり、
  同じゾーンのフロントの SSR から呼ぶには Service Binding が必要になる。不採用。
- **中継元の確認に Cloud Run の IAM 認証を使う（単独、または秘密ヘッダーとの併用）**: 上記「IAM 認証を使わない理由」のとおり。
  直接の大量アクセスを受けた場合の追加策とする（決定事項 7）。
- **中継元の確認に送信元 IP の制限・mTLS・Cloudflare Tunnel を使う**: 上記「中継元を秘密ヘッダーで確かめる理由」のとおり。不採用。
- **利用者 IP を `TRUSTED_PROXIES` と `X-Forwarded-For` で取る**: 送信元 IP で Worker を見分けられず、Cloudflare も
  利用者 IP の取得に `X-Forwarded-For` を推奨していない。不採用。
