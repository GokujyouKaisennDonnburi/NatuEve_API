# ADR-0010: UUID パースは handler 層に集約し、service 層は uuid.UUID を受け取る

- ステータス: Accepted
- 日付: 2026-07-09
- 関連: #112, ADR-0009

## コンテキストと課題

participation-logs の GET エンドポイント（#112）を追加した際、service 層の
`GetLatestStatus` が文字列の `eventID` / `profileID` を受け取り、内部で `uuid.Parse`
していた。これにより3つの問題が生じていた。

1. **ステータスコードの分裂**: 認証由来 `profileID` が不正 UUID のとき、service 層は
   `*ValidationError`（400 `invalid_request`）を返していた。しかし `profileID` は
   JWT の `sub` であり、リクエスト本文・パスに含まれないため、400 は「クライアントの
   リクエストが悪い」という誤解を招く。同一エンドポイントの POST 側（#111）は
   handler 層で `uuid.Parse` して 401 `unauthorized` を返しており、GET/POST で
   同一条件のステータスが分かれていた。

2. **Postgres 拒否形式の混入**: `uuid.Parse` は `urn:uuid:` 接頭辞付きの文字列を
   受理するが、Postgres の `uuid` 型は拒否する。service 層が `uuid.Parse` を通った
   生文字列をそのまま repository→SQL へ渡すと、Postgres が `invalid input syntax
   for type uuid` を返し、汎用エラー→500 になる。`parsedEventID.String()` で
   正規化文字列に直せば回避できるが、それは「パース済み UUID」を使っていることを
   前提とする設計であり、service 層が文字列を受け取る構造では見落としやすい。

3. **存在確認の用途外流用**: `requireEventExists` が `GetOwnerProfileID`（主催者取得
   クエリ）を「存在確認」として流用していた。owner 値は使わないのに主催者取得の
   セマンティクスに依存しており、将来 `GetOwnerProfileID` に非公開イベント除外等の
   仕様が加わると存在確認の意味が暗黙に壊れる。

これらは「service 層が文字列を受け取り、内部で UUID パースとドメインロジックを
混在させている」ことに根因がある。

## 決定

**UUID パラメータの `uuid.Parse` は handler 層で行い、service 層は `uuid.UUID` を
受け取る。** 存在確認には `EventRepository.Exists(ctx, eventID uuid.UUID)` を使う。

具体的には:

- handler 層: パスパラメータ・認証情報から取り出した文字列を `uuid.Parse` し、
  失敗時はエンドポイントの性質に応じたステータス（eventID 不正→400、
  authUser.ID 不正→401）を返す。service 層には `uuid.UUID` を渡す。
- service 層: `uuid.UUID` を受け取り、パース処理を持たない。
  存在確認は `Exists` を呼ぶ。
- repository 層: `Exists(ctx, eventID uuid.UUID) (bool, error)` を新設し、
  内部で `eventID.String()` を使って正規化文字列でクエリする。

## 理由

- **ステータスコードの正確性**: `profileID` は認証ミドルウェアが JWT から取り出した
  値であり、クライアントが直接制御できない。不正時は「リクエスト不正」（400）ではなく
  「認証情報不正」（401）が正しい。パース責務が handler にあれば、認証由来の値は
  401、パス由来の値は 400、と出所に応じたステータスを自然に返せる。service 層は
  値の出所を知らないため、この判断ができない。

- **Postgres 拒否形式の排除**: handler 層で `uuid.Parse` し `uuid.UUID` を下層へ
  渡すことで、repository が受け取るのは常に `uuid.UUID` 型になる。`uuid.UUID.String()`
  は Postgres が受理する正規化形式を返すため、`urn:uuid:` 等の問題が構造的に
  発生しない。

- **存在確認の意味論的正当性**: `Exists` は「イベントが存在するか」だけを問う専用
  メソッドであり、`GetOwnerProfileID` の主催者取得セマンティクスに依存しない。
  将来 `GetOwnerProfileID` に認可的な絞り込みが加わっても `Exists` は影響を受けない。

- **POST 側（#111）との一貫性**: 同一リソースの POST はすでに handler 層で
  `uuid.Parse` するパターンを採用している。GET を同じ構造に揃えることで、
  participation-logs の両エンドポイントが同一の責務分担になる。

## 影響（結果）

### 良い影響

- participation-logs の GET/POST で、同一条件のエラーが同一ステータスを返す。
- repository が受け取るのは常に `uuid.UUID` 型のため、正規化漏れによる 500 が
  構造的に防がれる。
- `EventRepository.Exists` は他の「認可チェック不要な存在確認」エンドポイント
  （将来の参照系等）でも再利用できる。
- service 層のテストが `uuid.UUID` 直受けになり、不正 UUID 文字列のパース失敗
  ケースを handler 側に委ねられるため、service テストはドメインロジックに集中できる。

### トレードオフ・制約

- **既存エンドポイント（`requireEventOwner` 系）は未移行**: `requireEventOwner` は
  service 層で `uuid.Parse` しており、本 ADR の指針に従っていない。これらを移行
  するには `requireEventOwner` のシグネチャ変更と呼び出し元3エンドポイント
  （notifications, reports, members）の handler 修正が必要で、影響範囲が大きいため
  別 issue で段階的に行う。移行完了までは新旧パターンが併存する。
- **handler 層にパース処理が分散する**: `uuid.Parse` が各 handler に現れるため、
  パース失敗時のエラーメッセージの統一には別途ヘルパーの検討余地がある
  （現状は各 handler が直接 `model.NewErrorResponse` を呼ぶ）。

## 検討した代替案

- **service 層でパースしつつステータスだけ 401 にする**: service 層が
  `*UnauthorizedError`（新設）を返すようにすればステータス分裂は解消できるが、
  service 層が「この値は認証由来だから 401」と出所を知る必要があり、層の責務が
  混乱する。また Postgres 拒否形式の問題（#2）は解消されないため不採用。
- **service 層でパースし `parsedEventID.String()` で正規化して渡す**: #2 の問題は
  解消するが、#1 のステータス分裂は残り、#3 の `GetOwnerProfileID` 流用も解消
  されない。部分解消に留まるため不採用。

## 関連コミット

- `b080c7a refactor: GET participation-logs を POST 側パターンに合わせる #112`
