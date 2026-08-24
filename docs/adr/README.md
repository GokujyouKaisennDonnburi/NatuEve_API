# Architecture Decision Records (ADR)

このディレクトリは、NatuEve_API における重要な設計上の意思決定を記録する。
「なぜその技術・構成を選んだのか」を後から追えるようにするためのもの。

## 運用ルール

- 1 ファイル = 1 決定。ファイル名は `NNNN-短いスラッグ.md`（連番は 4 桁ゼロ埋め）。
- 新規作成時は [`template.md`](./template.md) をコピーする。
- 一度 Accepted にした ADR は原則書き換えない。決定を覆す場合は新しい ADR を起こし、
  旧 ADR のステータスを `Superseded by ADR-XXXX` に更新する。
- ステータス: `Proposed`（提案中） / `Accepted`（採用） / `Deprecated`（非推奨） / `Superseded`（置換済み）。

## 一覧

| ADR | タイトル | ステータス |
|---|---|---|
| [0001](./0001-resend-transactional-email.md) | トランザクションメール基盤に Resend を採用 | Accepted |
| [0002](./0002-email-send-receive-separation-subdomain.md) | メール送受信の責務分離とサブドメイン戦略 | Accepted |
| [0003](./0003-api-key-least-privilege.md) | 外部サービス API キーは最小権限を原則とする | Accepted |
| [0004](./0004-bulk-send-as-individual.md) | 一斉送信は個別送信で行う（特定電子メール法対応） | Accepted |
| [0005](./0005-mail-rate-limit-handling.md) | メール送信のレート制限（429）対応方針 | Accepted |
| [0006](./0006-resend-sdk-user-agent-override.md) | resend-go SDK の User-Agent を独自値に上書きする | Accepted |
| [0007](./0007-bulk-notification-abuse-guard-scope.md) | 一斉送信の濫用対策はレート制限・送信量カウンタで行わない | Accepted 
| [0008](./0008-event-search-ilike-nfkc.md) | イベント検索は ILIKE 部分一致＋クエリ時 NFKC 正規化で実装する | Accepted |
| [0008](./0008-event-owner-check-unification.md) | イベント所有者が必要な操作の認可フローを統一する | Accepted |
| [0009](./0009-participation-log-event-not-found-status.md) | 参加状態ログ追加のイベント不存在は 404 で返す | Accepted |
| [0010](./0010-uuid-parse-in-handler-layer.md) | UUID パースは handler 層に集約し、service 層は uuid.UUID を受け取る | Accepted |
| [0011](./0011-event-leave-authenticated-only.md) | イベント参加キャンセルはログイン参加者に限定する | Accepted |
| [0012](./0012-event-cancellation.md) | イベントキャンセル（開催取りやめ）の状態管理と API 設計 | Accepted（一部 ADR-0016 で置換） |
| [0013](./0013-event-tag-association-on-create.md) | イベント投稿時のタグ紐づけ設計 | Accepted |
| [0014](./0014-event-tag-exposure-on-detail.md) | イベント詳細レスポンスでのタグ公開設計 | Accepted |
| [0015](./0015-event-tag-exposure-on-list.md) | イベント一覧レスポンスでのタグ公開設計 | Accepted |
| [0016](./0016-event-cancel-notification-outbox.md) | イベントキャンセル通知の Transactional Outbox 化と cancel API の非冪等化 | Accepted（決定事項 2 を ADR-0017 で置換） |
| [0017](./0017-event-cancel-notification-optional-content.md) | イベントキャンセル通知の件名・本文を任意化しサーバー既定文面で補う | Accepted |
| [0018](./0018-event-end-date-optional-with-default.md) | イベント終了日時を任意入力とし、省略時は開催日時で補完する | Accepted |
| [0019](./0019-event-search-keep-get-method.md) | イベント検索は GET を維持し、QUERY メソッドへは移行しない | Accepted |
| [0020](./0020-event-list-tag-filter.md) | イベント一覧のタグ絞り込みは OR 条件とし、検索条件を構造体へ集約する | Accepted |
| [0021](./0021-reject-invalid-query-string.md) | 不正なクエリ文字列はミドルウェアで 400 を返す | Accepted |
| [0022](./0022-event-member-profile-summary.md) | イベント参加者一覧にプロフィールサマリーを公開し、匿名参加は null で表す | Accepted |
| [0023](./0023-participant-category-breakdown.md) | 参加者カテゴリ別人数の内訳は event_costs を参照して保持する | Accepted |
| [0024](./0024-event-detail-participant-count.md) | イベント詳細で参加人数を公開し、残り人数はクライアントで算出する | Accepted |
| [0024](./0024-my-page-event-lists.md) | マイページのイベント一覧は種別つきの単一エンドポイントで返し、参加済みは終了日時で判定する | Accepted |
| [0025](./0025-profile-event-lists-public-scope.md) | プロフィールのイベント一覧は主催・参加済みのみ公開し、申し込み中は本人限定にする | Accepted |
| [0026](./0026-my-event-application-lookup.md) | 自分の申込内容は本人スコープの単一リソースで返し、参加費は含めない | Accepted |
| [0027](./0027-event-list-status-filter.md) | イベント一覧の開催状況絞り込みは排他な 3 値の status で表す | Accepted |
