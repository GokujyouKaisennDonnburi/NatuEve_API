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
長くなるため略...