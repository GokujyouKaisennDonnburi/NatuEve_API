# お問い合わせAPIの動作確認

設計は [ADR-0052](./adr/0052-contact-outbox.md) を参照。ページUIはWeb側で実装する。

## 準備

- DBのマイグレーションを適用する（`make migrate-up`）。
- `RESEND_API_KEY`・`MAIL_FROM`・`CONTACT_TO` を開発環境用に設定する。
  `CONTACT_TO` は自分が受信確認できる単一アドレス。実在の第三者をテスト先にしない。
- ローカルでは `OUTBOX_WORKER_ENABLED=true` を設定して起動する。
  本番は既存のCloud Schedulerを使う。APIが202を返しても配信設定がなければ届かない。
- ゲスト確認リンクには正しい `WEB_BASE_URL` が必要。

## AC（受け入れ基準）

1. 認証なしで次を送信し、202と `category` / trim済み `message` /
   RFC3339形式の `receivedAt` が返ること。

   ```bash
   curl -i -X POST http://localhost:8085/api/v1/contact \
     -H "Content-Type: application/json" \
     -d '{"category":"request","message":"デモ用の要望です"}'
   ```

2. `category` の省略・空文字・定義外、本文2001文字で400 / `invalid_request`。
   本文省略・2000文字は202。BodyLimit（1MiB）超過は413 / `request_too_large`。
3. `CONTACT_TO` の受信箱で `【お問い合わせ】要望` のメールを確認する。
   本文にカテゴリ、自由記述、受付日時があること。ローカルの巡回間隔は30秒だが、
   配信待ち・外部サービスの遅延を含む受信時間を保証するものではない。
4. 枠が空の同一IPから、**12秒未満で6回連続**リクエストして、6回目が
   429 / `rate_limited` になること。先の確認で枠を使った場合は60秒以上待つ。
   平均5回/分・burst 5のトークンバケットなので、間隔を空けると枠が回復する。
5. `CONTACT_TO` を外して再起動し、contactが404になること。確認後は元に戻す。
6. 申込可能な開発イベントへゲスト申込し、201と確認メールを確認する。
   宛先は受信できるテスト用アドレス、費用カテゴリはイベントに存在する値を使う。
   確認メールと、確認済みアドレスでの再申込案内メールの末尾に
   `お問い合わせ先: {CONTACT_TO}` が表示されること。
7. `make test` / `make lint` / `make swag-check` が通ること。
   DBテストは `DATABASE_URL` 未設定だとスキップするため、専用テストDBも使う。

## 運用上の確認

- 202はoutboxへの保存成功。同期送信ではなく、Resendエラーは既存ワーカーが再試行する。
- 管理者以外の宛先やReply-Toはリクエストで指定できない。
- outboxの `recipient_kind='email'` / `event_id IS NULL` がお問い合わせ。
  DBにある本文やメールアドレスをログ・スクリーンショット等に不用意に公開しない。
- マイグレーションを戻すとイベントIDのない行は削除される。送信待ちがないか確認する。
