# ADR-0008: イベント所有者が必要な操作の認可フローを統一する

- ステータス: Accepted
- 日付: 2026-07-08
- 関連: #105

## コンテキストと課題

`GET /api/v1/events/{id}/members`（#105）追加時に、認可ロジックと
「イベント不存在」のエラーコードが同じ目的のために書き方が 3 箇所に分裂しており、
それぞれ微妙に挙動がズレていることが判明した。

### 認可ロジックの現状（修正前）

「eventID をトリム → `GetOwnerProfileID` → `ErrNoRows` 判定 → `uuid.Parse` 比較 →
`ForbiddenError`」のフローが、以下の 3 サービスにコピペされていた。

- `service/event_notification.go`（`SendBulk`）
- `service/report_command.go`（`Create`）
- `service/event_join.go`（`ListMembers`、本 PR で追加）

各実装の差分:

- `event_notification.go`: `GetOwnerProfileID` のエラーを `sql.ErrNoRows` で判定
- `report_command.go`: 同上
- `event_join.go`: 同じく `sql.ErrNoRows` で判定

### エラーコードの現状

- `Join`（`POST /events/{id}/join`）: イベント不存在 → 404 (`*NotFoundError`)
- `SendBulk`（`POST /events/{id}/notifications`）: イベント不存在 → 400 (`*ValidationError`)
- `Create`（`POST /reports`）: イベント不存在 → 400 (`*ValidationError`)
- `ListMembers`（`GET /events/{id}/members`、本 PR で追加）: レビュー前 404 → 統一後 400

REST 的には「リソース不存在は 404」が正規だが、**認可が絡む場合の
「対象リソース不存在」と「権限なし」は意味が近い**。認可チェック内で
両方を行うと区別がつかず、handler の分岐も複雑化する。

### 決めるべきこと

1. 認可ロジック（イベント所有者の確認と 400/403 変換）をどこに置くか
2. 認可チェックの延長で「イベント不存在」を何で返すか
3. 既存の Join の 404 を巻き戻すか / 据え置きにするか

## 決定

**イベント所有者を確認する操作では、以下の 2 点を必須ルールとして統一する。**

1. 認可ロジックは `service.requireEventOwner` ヘルパーに集約する。
   - 入力: `(ctx, eventRepo, profileID, eventID)`
   - 成功時: パース済み `eventID uuid.UUID` を返す
   - 失敗時: 以下の型付きエラーのいずれかを返す
     - `*ValidationError`（400）: eventID のパース失敗 or イベント不存在
     - `*ForbiddenError`（403）: profileID のパース失敗 or owner 不一致
2. イベント不存在は **HTTP 400 (`invalid_request`)** で返す。
   REST 的な 404 よりも、認可チェックの延長で発生する例外としての一貫性を優先する。
3. `Join`（イベント参加申込）の 404 は**巻き戻さない**。Join は認可チェックを伴わない
   （匿名参加可・ログイン時は profileId を記録するだけ）別系統のロジックであり、
   イベント不存在が 404 のままで意味的に妥当。`GetOwnerProfileID` を経由しない。

ヘルパーの責務:

- `strings.TrimSpace` → `uuid.Parse` の順で eventID を検証
- profileID の `uuid.Parse` 失敗は `ForbiddenError`（fail-closed）
- repo の `GetOwnerProfileID` は `repository.ErrEventNotFound` センチネルで「不存在」を返す
- service 層は `sql.ErrNoRows` を直接見ない（repository 層でラップして吸収する）

## 理由

- **認可ロジックの 3 箇所コピペは修正漏れのリスクが直接セキュリティホールに直結する**。
  1 箇所に集約すれば、fail-closed 挙動（不正な入力で通す/通さないの境界）の変更が
  全エンドポイントに必ず反映され、レビュー観点でも 1 ファイル見れば全容が把握できる。
- **イベント不存在を 400 で返す方が認可フローと整合する**。handler の責務が
  「入力検証エラー」「認可エラー」「想定外エラー」の 3 種に収まり、404 の特例が消える。
  クライアント側も `invalid_request` の共通処理に乗せられ、エンドポイントごとに
  分岐が要らなくなる。
- **Join の 404 は据え置きにする**。Join は匿名参加を許容する性質上、認可チェックを
  通さず `repository.ErrEventNotFound` を直接 404 に変換する。`GetOwnerProfileID`
  を経由しないため、ヘルパーの集約対象にも含めない。コードベースには 2 つの流儀
  （認可系=400、存在参照系=404）が併存することになるが、それぞれ独立した根拠がある
  ため許容する。

## 影響（結果）

### 良い影響

- 認可ロジックの変更時に、修正漏れで「特定エンドポイントだけ通す/通さない」が
  発生する余地が消える。
- クライアント側のエラーハンドリングが単純になる（認可が必要な操作は
  400/403 のみを考えればよい）。
- 同じ理由で `database/sql` を service 層が直接参照することがなくなり、
  層分離が強化される（repository 実装差し替え時の 500 化けリスクを排除）。

### トレードオフ・制約

- REST 的な観点からは「リソース不存在 = 404」が正規であり、本 ADR はそれを
  認可系の操作に限定して 400 に倒す選択をする。`GET /api/v1/events/{id}` のような
  **認可を伴わない純粋なリソース取得は 404 のまま**にする方針を採る（現在も 404）。
- 既に公開済みの `POST /events/{id}/notifications` と `POST /reports` の挙動を
  変えるわけではないが、ヘルパー移行により**内部実装の依存関係が変わる**。
  エラーメッセージ文言が微妙に変わる可能性がある（例: 「指定されたイベントが
  存在しません」の prefix など）。クライアントが `code: "invalid_request"` で
  判定している前提に依存する。
- コードベースには 400 / 404 の **2 つの流儀が併存**する。将来的に 404 へ統一する
  場合は別 ADR を起こす。

## 検討した代替案

- **404 に統一する**: 意味的に最も自然だが、`POST /events/{id}/notifications` と
  `POST /reports` の既存挙動（400）を巻き戻す破壊的変更になり、
  クライアント側のハンドリング変更も必要になる。本 PR のスコープを超えるため不採用。
  採用するなら別 ADR で利用者合意を取った上で段階移行すべき。
- **ヘルパー化せず 3 箇所に重複を残したまま、エラーコードだけ 400 に揃える**:
  fail-closed 条件のドリフトは防げず、レビュー指摘 2 が解決しない。コスト対効果が
  ないため不採用。
- **Join の 404 も 400 に揃える**: 認可を伴わない Join で 400 にすると
  「入力中にイベントIDがあるが、そのイベントが今無い」状況のシグナルが弱くなる。
  既存挙動を変えずに済むため、Join は据え置きが妥当。

## 関連コミット

- 認可ロジック集約: `099d68d refactor: イベント所有者チェックを requireEventOwner ヘルパーに集約 #105`
- OpenAPI 更新: `6157df3 docs: 所有者チェック統一に伴うswaggerを再生成 #105`
