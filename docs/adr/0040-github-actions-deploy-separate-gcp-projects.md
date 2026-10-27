# ADR-0040: デプロイを GitHub Actions で自動化し、検証環境と本番を別の GCP プロジェクトに置く

- ステータス: Accepted
- 日付: 2026-10-26
- 関連: #219, ADR-0003, ADR-0034, ADR-0036, ADR-0037, ADR-0039
- 被変更: 決定事項 8 の Resend（検証用の送信専用キーを別に発行し、送信元のドメインは本番と同じにする）は [ADR-0041](./0041-staging-shares-auth-mail-with-dev.md) で変更した（開発と共用のキーと送信元 `dev-mail.natuportal.org` を使う）。「理由」の 8 と「トレードオフ・制約」の「検証環境のメールは本番と同じドメインから送られ」は、変更前の決定を前提にした記述

## コンテキストと課題

ADR-0036 で「main へのマージで検証環境へ、承認後に同じイメージを本番へ」と決め、次の点を #219 に任せた。

- CI/CD の実行方法と、GCP への認証方式
- ワークフローの形、承認待ちが溜まったときに古いイメージを出さない仕組み、Prevent self-review を使うか
- GCP のプロジェクトを本番と分けるか、Resend の送信元、検証用 Worker のドメイン名

前提:

- CI は GitHub Actions で動いている（`.github/workflows/ci.yml`）。リポジトリは public、Organization は Free プラン。
- 本番の承認には GitHub Environments の `production` を使う（ADR-0036 決定事項 3）。
- 本番の公開とデプロイは同時に行う。初回の本番デプロイもこの仕組みで出す。

## 決定

**GitHub Actions で、main への push ごとに 1 本の実行で「ビルド → 検証環境 → 承認 → 本番」を行う。
GCP のプロジェクトは検証環境と本番で分ける。**

1. CI/CD は GitHub Actions で行う。サードパーティの Action（google-github-actions・docker・oven-sh）はコミットの SHA で固定する。
2. GCP への認証は Workload Identity Federation で行い、サービスアカウントの鍵は発行しない。
   本番用のサービスアカウントは、GitHub の OIDC トークンの `sub` が
   `repo:GokujyouKaisennDonnburi/NatuEve_API:environment:production` のときだけ使える。検証用は `environment:staging` に限る。
3. GCP のプロジェクトは検証環境と本番で分ける。
   イメージは検証用プロジェクトの Artifact Registry に 1 回だけ置き、本番のジョブが同じダイジェストのまま本番用プロジェクトの Artifact Registry へコピーする。
   イメージには provenance と SBOM を付けず、1 つのマニフェストにする。
4. ワークフローは main への push を起点に、同じ実行の中で次の順に行う。
   1. ビルドし、検証用の Artifact Registry に置く。
   2. 検証環境: マイグレーション → Cloud Run へのデプロイ → Worker のデプロイ。
   3. `production` 環境の承認を待つ。
   4. 本番: main の最新か確かめる → イメージのコピー → マイグレーション → Cloud Run へのデプロイ → Worker のデプロイ。

   設定（GitHub Environments の変数・シークレット）だけを反映するときは、main を対象に手動で実行する（`workflow_dispatch`）。
5. 検証環境と本番のジョブは最初に、その実行のコミットが main の最新かを確かめ、最新でなければ失敗させる。
6. `production` 環境で Prevent self-review を有効にする。
7. マイグレーションに使う `DATABASE_URL` は、各プロジェクトの Secret Manager から読む。
   Worker のデプロイに使う Cloudflare の API トークンは、GitHub Environments の各環境のシークレットに置く。
8. 検証環境の Worker のドメインは `natueve-api-stg.natuportal.org`。
   Resend は検証用の送信専用キーを別に発行し、送信元のドメインは本番と同じにする。
   R2 はローカル開発用のバケット（`assets-dev.natuportal.org`）を使う（ADR-0039）。
9. Cloud Run の環境変数は、値の種類ごとに置き場所を分ける。
   - Deployment.md に値が載っている公開の設定: 環境ごとのファイル（`deploy/cloud-run/<環境>.yaml`）
   - ID 類（プロジェクト ID・アカウント ID・バケット名など）: GitHub Environments の変数
   - run.app の URL を組み立てられる値（Cloud Run のサービス名・Scheduler の audience）: GitHub Environments のシークレット。
     デプロイの出力に含まれる run.app の URL は、置き換えてからログに出す
   - 秘密値: Secret Manager

## 理由

- 1: 承認に GitHub Environments を使うため、同じ実行の中で承認待ちにできる。既存の CI も GitHub Actions で、仕組みが 1 つで済む。
  本番の権限と Cloudflare のトークンを渡すジョブで使うため、後から付け替えられるタグではなく SHA で固定する。
- 2: 鍵を発行しないため、鍵の漏えい・失効を管理しなくてよい（ADR-0003）。`environment` で絞ると、承認前のジョブは本番の権限を得られない。
  GitHub Environments のシークレットも、保護ルールを通ったジョブにしか渡らない。
- 3: 権限の境界がプロジェクトで分かれ、検証環境の設定や権限の誤りが本番に届かない。Cloud Run の無料枠は請求アカウント単位で、分けても費用は変わらない。
  コピーを本番のジョブで行うため、承認前のジョブは本番のプロジェクトに書き込まない。
  docker/build-push-action は既定で provenance を付け、イメージのインデックスを作る。1 つのマニフェストにすると、ビルドの出力の digest が
  そのまま Cloud Run に渡すイメージの digest になり、コピー後の確認とデプロイの指定に同じ値を使える。
- 5: 承認待ちの実行が複数溜まったとき、古い実行を後から承認しても、新しいイメージを古いもので上書きしない。
  検証環境も、CI の完了の順番が前後したときに、古いコミットで新しい状態を上書きしない。
- 6: マージした本人だけでは本番に出せず、本番に出す前に必ず 2 人目が確かめる。
- 7: 秘密情報の置き場所を Secret Manager にまとめ、Cloud Run と CI で同じ値を使う。Cloudflare のトークンは GCP の外にあるため、GitHub の環境ごとのシークレットに置く。
- 8: 送信元のドメインを本番と同じにすると、SPF・DKIM などの DNS の設定を足さなくてよい。
- 9: 公開の設定をファイルに置くと、変更が PR でレビューされ、検証環境 → 承認 → 本番の流れを通る。
  リポジトリは public で、Actions のログは誰でも読める。GitHub の変数はステップのログにそのまま出るが、シークレットは伏せられる。
  run.app の URL は「サービス名-プロジェクト番号.リージョン.run.app」の形で、プロジェクト番号は Workload Identity の設定値としてログに出る。
  ADR-0034 は run.app の URL を公開しない前提で、知られた場合の対処（サービスを別名で作り直す）もこの前提に立つ。

## 影響（結果）

### 良い影響

- 本番の権限は承認を通ったジョブだけが持ち、手元から本番へデプロイしなくてよくなる。
- 検証環境と本番に、同じダイジェストのイメージが出る。

### トレードオフ・制約

- マージした本人は承認できない。ほかの承認者がいないと本番に出せない。
- 承認待ちの間に次のマージがあると、古い実行の本番のジョブは失敗する。最新の実行を承認する。
  後のコミットで CI が失敗すると、それを直すコミットが入るまで本番に出せない。
- サードパーティの Action の更新は、SHA を手で差し替える。
- GCP のプロジェクトが 2 つになり、サービスアカウント・Secret Manager・Workload Identity の設定をそれぞれ行う。
  Secret Manager の無料枠は本番でほぼ使うため、検証用のシークレットは少額の課金になる（ADR-0036）。
- 検証環境のメールは本番と同じドメインから送られ、受け取った側からは見分けにくい。
- イメージのコピーでダイジェストが変わらないことは、実装時に確かめる。
- Worker は変更がなくても毎回デプロイする。

## 検討した代替案

- **Cloud Build**: 承認が GitHub Environments とは別の仕組みになり、ADR-0036 の決定と二重になる。不採用。
- **サービスアカウントの鍵（JSON）を GitHub のシークレットに置く**: 鍵の漏えい・失効の管理が要る。不採用。
- **1 つの GCP プロジェクトで、サービスアカウントだけを分ける**: プロジェクトの数は減るが、プロジェクト単位の権限を付けると検証用のアカウントが本番に届く。
  権限の付け方の誤りに弱い。不採用。
- **タグの push を起点に本番へ出す**: 承認の前にタグを打つ操作が増え、どのイメージを出すかが実行とタグの 2 か所で決まる。不採用。
- **イメージを本番用プロジェクトの Artifact Registry に置き、検証環境もそこから使う**: コピーが要らないが、承認前のビルドのジョブが本番のプロジェクトに書き込む。不採用。
- **検証環境の送信元を検証用のサブドメインにする**: メールを見分けやすいが、DNS の設定が要る。不採用。
