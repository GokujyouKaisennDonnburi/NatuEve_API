# ADR-0032: 本番デプロイ構成を Cloud Run＋Cloud Scheduler＋Supabase（DB・Auth）とする

- ステータス: Accepted
- 日付: 2026-10-13
- 関連: #212, ADR-0001, ADR-0016, ADR-0031
- 変更: ADR-0016 の決定事項 4（送信ワーカーを goroutine 1 本で常駐させる）を置き換える（Supersedes, 部分的）。
  `.claude/rules/docker.md` にあった「Xserver VPS + Docker Compose、1 年後に AWS（App Runner/ECS + RDS/Aurora）へ移行」という前提を撤回する
- 構成の詳細・設定値・月額試算は [docs/Deployment.md](../Deployment.md) を参照
- 注記: 決定事項 2 の実装方式（内部エンドポイントの認証・排他・ローカル開発での起動方法）は [ADR-0035](./0035-outbox-scheduler-endpoint.md) で決めた。
  30 秒ティッカーの常駐ワーカーは、ローカル開発用に `OUTBOX_WORKER_ENABLED=true` のときだけ残している（本番では使わない）
- 被変更: 「トレードオフ・制約」の「本番公開時は Supabase Pro が前提」は [ADR-0036](./0036-github-flow-staging-approval.md) で撤回した
  （本番も Free のまま運用し、バックアップは一旦取らない）。
  「理由」の運用負担と「Supabase を DB にする理由」に挙げた Supabase Pro の日次バックアップは、本番では得られない

## コンテキストと課題

バックエンド API の本番デプロイ先が未決だった。従来は「Xserver VPS + Docker Compose で運用し、
1 年後に AWS へ移行する」前提で各 ADR が書かれていたが、この前提ごと見直すことになった。

デプロイ先の選定に効くアプリ側の要件・制約は次のとおり。

- API は Go の単一バイナリで、distroless の Docker イメージとして動く。
- DB は PostgreSQL 17 で、拡張 `pg_trgm` を使う（ADR-0030）。
- 通知メールは Transactional Outbox（ADR-0016）で送る。送信ワーカーは API プロセス内に常駐する
  goroutine で、30 秒間隔でポーリングする。単一プロセス前提で、複数インスタンスでの並行処理には
  対応していない。
- 認証は Supabase Auth の JWT を JWKS で検証している（NatuPortal の複数プロダクトで共有）。
- ファイルは Cloudflare R2、メールは Resend（ADR-0001）を使う。
- 利用者は国内。立ち上げ期で、トラフィックは小規模。費用は最小にしたい。

Cloud Run の課金方式には次の制約がある（公式ドキュメントで確認）。

- リクエスト課金では、CPU はリクエスト処理中にしか割り当てられない。常駐 goroutine は動作が保証されない。
- インスタンス課金（CPU 常時割り当て）では 1 vCPU 未満を選べず、メモリも 512MiB 以上が必要で、
  アイドル時間も課金される。

## 決定

**API は Cloud Run（東京リージョン）にリクエスト課金・最小インスタンス 0 で載せる。outbox の処理は常駐
goroutine をやめ、Cloud Scheduler からの定期呼び出しで起動する。DB と認証には Supabase（東京リージョン）を使う。**

1. **API の実行基盤**: Cloud Run（`asia-northeast1`）。課金はリクエスト課金、最小インスタンス数は 0。
2. **outbox の起動方式**: API プロセス内の常駐ワーカー（30 秒ティッカー＋`Wake()`）を廃止する。
   Cloud Scheduler が 1 分間隔で「outbox を 1 回処理する」内部エンドポイントを呼ぶ方式に変える。
   - 内部エンドポイントは Cloud Scheduler 以外から呼べないよう認証する。
   - outbox 処理が同時に複数走っても二重送信しないよう、排他を入れる。
   - `Wake()` による即時送信は行わない。送信は最大で起動間隔（1 分）ぶん遅れる。
   - 認証方式・排他方式・ローカル開発での起動方法は、実装時に別途決める。
3. **DB**: Supabase の PostgreSQL（`ap-northeast-1`）。Supabase は Postgres のホストとしてのみ使う。
   - Supabase の Data API（PostgREST）から NatuEve のテーブルに到達できない状態にする
     （Data API の無効化、または API 用ロールへの権限付与をしない）。DB へのアクセスは必ず API サーバーを通す。
   - Supabase Storage は使わない（ファイルは引き続き Cloudflare R2）。
4. **認証**: Supabase Auth（従来どおり JWKS で JWT を検証する）。
5. **メール基盤**: 本 ADR では変更しない（Resend を継続、ADR-0001）。

## 理由

- **費用**: 小規模では Cloud Run の無料枠（月 18 万 vCPU 秒・200 万リクエスト）に収まり、サーバー代がほぼ 0 になる。
  例えば月 10 万リクエスト（1 件 0.1 秒）に、Scheduler の呼び出し（月 約 4.3 万回）を 1 回 1 秒と多めに見積もって足しても
  約 5.3 万 vCPU 秒で、無料枠の 3 割程度に収まる。
  月額はほぼ Supabase のプラン（Free $0／Pro $25）だけで決まる（2026-10 時点の試算）。
- **outbox を Scheduler 起動にする理由**: 常駐ワーカーのまま Cloud Run に載せるとインスタンス課金・常時 1 台が必要で、
  1 vCPU 必須のため月 約 $44 かかる。起動を外に出せばリクエスト課金のまま 0 台までスケールダウンできる。
  Outbox による「キャンセルが成立したら通知も必ず送られる」保証（ADR-0016）は、起動方式を変えても維持される。
- **運用負担**: Cloud Run・Supabase ともマネージドで、OS 更新・DB バックアップ（Supabase Pro は日次）・TLS を自前で持たなくてよい。
- **レイテンシ**: Cloud Run と Supabase を同じ東京リージョンに置ける。API から DB への往復が国内で完結する。
- **Supabase を DB にする理由**: 認証で既に使っており、東京リージョンがあり、Pro プランに日次バックアップが含まれる。
- **Data API を塞ぐ理由**: Supabase の既定では、`public` スキーマのテーブルに API 用ロールの読み書き権限が付く。
  RLS のないテーブルは、公開されている anon キーで REST から直接読み書きできてしまう。
  CLAUDE.md の「座標情報の保護」「ドメインロジックの集約」は、DB への経路が API サーバーだけであることが前提なので、別経路を作らない。

## 影響（結果）

### 良い影響

- 小規模の間は、月額がほぼ Supabase のプラン料金だけになる。
- outbox の処理が API の常駐プロセスから切り離されるため、API を複数インスタンスにスケールアウトできるようになる
  （排他の実装が前提）。
- Docker イメージは基盤に依存しないまま保てる。Scheduler への依存は「HTTP エンドポイントを定期的に呼ぶ」だけなので、
  他の定期実行の仕組みにも置き換えられる。

### トレードオフ・制約

- **通知メールの遅延**: `Wake()` をやめるため、キャンセル通知・欠席連絡のメールは最大で起動間隔（1 分）遅れる。
- **outbox の Scheduler 化が実装されるまで、本構成では通知メールの送信が保証されない**。リクエスト課金では、常駐ワーカーは
  リクエスト処理中にしか CPU を得られない。インスタンスが 0 台になればワーカーも止まる。また、複数インスタンスでは二重送信の恐れがある。
  Scheduler 化の実装が本番公開の前提条件になる。
- **排他が必須**: Scheduler の再試行や処理の長期化で outbox 処理が並行しうる。現実装（`FOR UPDATE SKIP LOCKED` なし）
  のままでは二重送信が起きる。
- **コールドスタート**: 最小インスタンス 0 のため、アイドル後の最初のリクエストで起動待ちが発生する。
- **Supabase への接続方式に制約がある**: IPv6・prepared statement の制約があり、検証して接続方式を決める必要がある
  （詳細は [docs/Deployment.md](../Deployment.md) の未決事項）。
- **本番公開時は Supabase Pro が前提**: Free には自動バックアップがなく、一時停止もある（制限の詳細は Deployment.md）。
- **Supabase プロジェクトの分け方が未決**: Auth は NatuPortal の複数プロダクトで共有しているため、DB を同じプロジェクトに
  置くか別にするかで、Data API を塞ぐ影響範囲と費用が変わる（詳細は Deployment.md の未決事項）。
- **関係するベンダーが増える**: Google Cloud・Supabase・Cloudflare（R2）・Resend の 4 社になる。

## 検討した代替案

- **Cloud Run をインスタンス課金・最小 1 台で常時起動する**: コード変更は不要だが、1 vCPU 必須で月 約 $44 かかる。不採用。
- **Fly.io（東京）**: 512MB を常時起動して月 約 $5。コード変更も不要。Scheduler 化した Cloud Run（無料枠内）より高く、
  常駐ワーカーの単一プロセス前提も残るため不採用。
- **VPS（ConoHa／さくら）に API と PostgreSQL を同居させる**: DB 込みで月 700〜2,000 円程度と絶対額では最安。
  ただし OS 更新・DB バックアップ・TLS・監視を自前で運用する必要があり、不採用。
- **Cloudflare Containers（Workers Paid）**: 月 $5〜12。起動場所を東京に固定できず、東京の DB との往復遅延が読めない。
  稼働時間の保証もなく、前段に TypeScript の Worker が必要になる。不採用。
- **Render／Railway／Koyeb**: 東京リージョンがない（アジアはシンガポールまで）。不採用。
- **Neon（DB）**: 東京リージョンがない。また outbox の定期処理で DB が休止しないため、無料枠（100 CU 時間）を超える。不採用。
- **AWS（App Runner／ECS＋RDS/Aurora）**: 旧計画。移行前提ごと撤回した。小規模では上記より割高なため不採用。
