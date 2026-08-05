# ADR-0022: イベント参加者一覧にプロフィールサマリーを公開し、匿名参加は null で表す

- ステータス: Accepted
- 日付: 2026-08-05
- 関連: #162, ADR-0011, ADR-0015

## コンテキストと課題

`GET /api/v1/events/{id}/members`（`EventMemberListResponse`）は、主催者のみが自分のイベントの
参加者一覧を取得できるエンドポイント（認可は ADR-0008 の `requireEventOwner` に集約）。現在返している
のは参加者 1 人あたり `username` / `profileId` / `partySize` / `mailAddress` / `createdAt` で、
プロフィールアイコンは含まれていない。#162 で参加者一覧画面にアイコンを表示する要件が生じた。

前提となるスキーマは次の通り。

- `event_members.profile_id` は nullable で `profiles(id)` への FK を持つ。ログイン参加なら値が入り、
  匿名参加なら NULL（ADR-0011 が「参加は匿名可・キャンセルはログイン必須」としているため、匿名参加は
  例外ケースではなく設計上の常態）。
- `profiles` は `id`（PK）・`display_name`・`avatar_url`（いずれも nullable）を持つ。
- `event_members` には `(event_id, created_at)` の複合インデックスがある（migration 20260708052650）。
- 公開用 DTO `model.ProfileSummary`（`id` + `displayName` + `avatarUrl`）は既にあり、
  `EventSummary.Profile` / `EventResponse.Profile` で使われている。

決めるべき点は以下だった。

1. アイコンをどう取得するか（参加者ごとの追加クエリか、本体クエリへの JOIN か）。
2. レスポンスの形をどうするか（トップレベルにフラットに足すか、ネストしたオブジェクトにするか）。
3. 匿名参加（`profile_id` が NULL）をどう表現するか（`null` か、全フィールド空のオブジェクトか）。

3 が特に論点だった。既存の `EventSummary.Profile` は非ポインタの `ProfileSummary` で、NULL カラムを
空文字で埋めるため、プロフィールが無くても `{"id":"","displayName":"","avatarUrl":""}` を返す実装に
なっている。ここに機械的に揃えるかどうかを判断する必要があった。

## 決定

**`ListMembers` の本体クエリに `LEFT JOIN profiles` を足して 1 クエリでプロフィールを同時取得し、
`EventMemberResponse` に既存の `ProfileSummary` 型を再利用したネストした `profile` フィールドを
追加する。匿名参加（`profile_id` が NULL）は `profile: null` を返す（ポインタ型）。既存フィールドは
一切変更しない純粋な追加とする。**

1. リポジトリの `ListMembers` を
   `FROM event_members m LEFT JOIN profiles p ON p.id = m.profile_id` に変更し、
   `p.id` / `p.display_name` / `p.avatar_url` を同時に SELECT する。往復は 1 回のまま。
2. `model.EventMember`（DB 行モデル）と `model.EventMemberResponse`（DTO）の双方に
   `Profile *ProfileSummary` を持たせる。DTO のタグは `json:"profile"` とし、`omitempty` は付けない。
3. `p.id` が NULL のとき（＝匿名参加）は `Profile` を nil のままにし、JSON では `null` を返す。
   `EventSummary.Profile` の空オブジェクト表現とは**意図的に異なる**扱いにする。
4. 既存の `profileId` は互換性のためそのまま残す（`profile.id` と重複するが破壊的変更を避ける）。

## 理由

### JOIN で 1 クエリにまとめる

`profiles.id` は PRIMARY KEY のため、JOIN は参加者 1 行あたり PK ルックアップ 1 回で済む。
`WHERE m.event_id = $1 ORDER BY m.created_at` は既存の `(event_id, created_at)` 複合インデックスが
そのまま効き続けるので、JOIN を足しても走査・ソートのコストは変わらず、追加インデックスも不要。

参加者ごとに `ProfileRepository.GetByID` を呼ぶ実装なら repository のクエリに触らずに済むが、参加者 N 人で
N 往復（N+1）になる。ADR-0015 がタグ公開で N+1 を避けたのと同じ判断。ただしタグと違い `profiles` は
参加者に対して 1 対 1 なので、一括取得ヘルパー（`attachTagsToSummaries` 相当）すら不要で、本体クエリへの
`LEFT JOIN` 1 つで完結する点が異なる。

アイコンの実体についても追加コストは無い。`profiles.avatar_url` には Supabase 由来の外部 URL 文字列が
そのまま入っており（`repository/profile.go` の `Update` が `avatar_url` を更新対象から外しているのは
この値が Supabase 由来だから）、R2 の署名 URL ではない。したがって参加者ごとに署名 URL を発行するような
コストは発生せず、DB から読んだ文字列をそのまま返せる。

### ネストしたオブジェクトにする

`EventMemberResponse` には既に `username` があり、これは**申込フォームで入力された名前**で、
プロフィールの `displayName`（アカウントの表示名）とは別物。トップレベルに `avatarUrl` / `displayName` を
フラットに並べると、どちらの名前なのかがフィールド名から読み取れない。ネストすれば
「`profile` 配下はアカウント由来、それ以外は申込由来」と出自が構造で表現でき、将来プロフィール項目を
足したくなってもトップレベルが太らない。既存の `ProfileSummary` をそのまま再利用できるため、
イベント側のレスポンスとも表現が揃い、OpenAPI から生成する型も 1 つで済む。

### 匿名参加を null にする（本 ADR の主要な判断）

- **事実に忠実だから**。匿名参加者には `profiles` の行が本当に存在しない。
  `{"id":"","displayName":"","avatarUrl":""}` は「存在しないもの」を「全フィールドが空の存在するもの」
  として表現することになり、レスポンスがデータの実態と食い違う。`null` なら「無い」をそのまま表せる。
- **空オブジェクトだと 2 つの状態が潰れるから**。`profiles.avatar_url` は nullable なので、
  「匿名参加でプロフィールが存在しない」と「ログイン済みだがアイコン未設定」が、空オブジェクト表現では
  どちらも `avatarUrl: ""` になり区別できない。`null` なら
  `profile === null` が匿名、`profile !== null && avatarUrl === ""` が未設定と一意に読める。
- **クライアント側の型で null 処理が強制されるから**。OpenAPI から型を生成したとき
  `ProfileSummary | null` になり、参加者一覧を描画するコードは必ず分岐を書くことになる。
  空オブジェクトだと `member.profile.avatarUrl` がコンパイルを通ってしまい、匿名参加者の行で
  `src=""` の壊れた画像を出す事故がすり抜ける。参加者一覧は匿名参加者が普通に混ざる画面なので、
  この事故は例外的な状況ではなく通常運用で起きる。
  ただし swag が生成するのは Swagger 2.0 で、Go のポインタ型から nullable を自動推論しない。
  そのため DTO のタグに `extensions:"x-nullable"` を付けて `x-nullable: true` を出力させる
  （`ParticipationStatusResponse` の `action` / `updatedAt` が既に同じ方法を採っている）。
  このタグが無いと生成される型は非 null になり、本項の理由自体が成立しなくなる。
- **既存の空オブジェクト表現が、意図して選ばれた仕様ではないから**。`EventSummary.Profile` が
  空オブジェクトを返し得るのは `events.profile_id` が nullable だからだが、イベント作成は認証必須のため
  実運用で NULL になることはなく、空オブジェクトが実際にレスポンスへ出ることはない。つまりあの表現は
  「起こらないケースの副産物」であって、匿名を空オブジェクトで表すという決定が過去に下されたわけではない。
  対して `event_members.profile_id` の NULL は設計上の常態なので、機械的に踏襲する理由がない。
- なお FK があるため、`profile_id` が非 NULL なら `profiles` の行は必ず存在する。よって
  `profile` が `null` になるのは `profileId` が `null` のときだけであり、両者は常に一致する。
  クライアントはどちらを見て匿名判定しても同じ結果になる。

### 既存フィールドを変えない

`profileId` は `profile.id` と内容が重複するが、既にクライアントが参照している可能性があるため残す。
本変更をフィールド追加のみに留めることで、破壊的変更なしにリリースできる。

## 影響（結果）

### 良い影響

- 往復数・インデックスを増やさずに（本体クエリ 1 回のまま）参加者のアイコンと表示名を返せる。
- 匿名参加とアイコン未設定をクライアントが区別できる。デフォルトアイコンの出し分けやバッジ表示など、
  UI 側で意味のある分岐が書ける。
- 生成される型が `ProfileSummary | null` になり、null 処理の漏れがコンパイル時に検出される。
- 既存フィールド不変のため破壊的変更にならず、クライアントは自分のタイミングで `profile` を使い始められる。
- 「申込時の名前（`username`）」と「アカウントの表示名（`profile.displayName`）」の違いが
  レスポンス構造で表現される。

### トレードオフ・制約

- **同じ `ProfileSummary` でもエンドポイントごとに null 可否が割れる**。`EventSummary.Profile` /
  `EventResponse.Profile` は非 null（空オブジェクト表現）、`EventMemberResponse.Profile` は null 可。
  クライアントは 2 種類の扱いを意識する必要がある。前述の通りイベント側で空オブジェクトが実際に出ることは
  ないため実害は小さいが、表記の不揃いは残る。イベント側も null 表現へ寄せるなら別 ADR で扱う。
- **`profileId` と `profile.id` が重複する**。互換性維持を優先した結果で、`profileId` を落とすなら
  破壊的変更として別途扱う。
- **null 表明はベンダー拡張に依存する**。`x-nullable` は Swagger 2.0 の拡張であり、クライアント側の
  コード生成器がこれを解釈するかに依存する。解釈しない生成器では型で null 処理を強制できず、
  `@Description` の記述が頼りになる。なお同じ構造体の `profileId` には `x-nullable` が付いておらず
  （本 ADR 以前からの状態）、同一レスポンス内で null 表明の有無が揃っていない。
- **`model.EventMember` が読み書き兼用のまま読み取り専用フィールドを持つ**。`EventMember` は
  `Join`（INSERT）と `ListMembers`（SELECT）で共用しており、`Profile` は Join 経路では常に nil。
  `ListMembers` 専用の型を切る案もあったが、`EventSummary` が同じ持ち方をしており、型を増やすほどの
  利得がないと判断した。
- **アイコンは初回ログイン時の値で固定される**。`profiles` の Upsert は
  `ON CONFLICT DO UPDATE SET email, updated_at` のみで `avatar_url` を上書きしないため、Supabase 側で
  アイコンを変更しても API が返す値は追随しない。本 ADR 以前からの既存挙動で、本変更で新たに生じた制約では
  ないが、参加者一覧にアイコンを出すことで初めてユーザーの目に触れるようになる。追随が必要になれば別途扱う。
- **主催者のみが見られる情報が 1 つ増える**。本エンドポイントは `requireEventOwner` 配下で主催者限定の
  ため公開範囲は広がらないが、返す個人情報の量は増える（表示名・アイコン URL）。第三者に開く一覧を将来
  作る場合は、`mailAddress` と併せて公開範囲を再検討すること。

## 検討した代替案

- **トップレベルに `avatarUrl` だけフラットに追加する**: 差分は最小で済むが、既存の `username` との
  関係が読み取れず「誰の名前・誰のアイコンか」がフィールド名から判別できない。プロフィール項目を足すたびに
  トップレベルが太り、他エンドポイントの `profile` 表現とも割れる。不採用。
- **ネストしつつ匿名も空オブジェクトを返す（`EventSummary` と統一）**: 型表記が全エンドポイントで揃う
  利点はあるが、匿名参加とアイコン未設定が区別できず、生成した型でも null 処理が強制されない。揃える対象の
  既存表現自体が意図的な決定ではないため、揃えることの価値が薄いと判断した。不採用。
- **参加者ごとに `ProfileRepository.GetByID` を呼ぶ**: service 層だけで完結し repository のクエリに
  触らずに済むが、参加者 N 人で N 往復（N+1）になる。ADR-0015 と同じ理由で不採用。
- **`profileId` だけ返し、クライアントが参加者ごとに `GET /api/v1/profiles/{id}` を叩く**:
  サーバー変更は不要だが、一覧を開くたびに参加者数ぶんの往復が発生する。表示要件に対して明らかに非効率。不採用。
- **`omitempty` を付けて匿名時はキーごと省略する**: 不採用。理由は 3 つある。

  1 つ目は**得るものが無い**こと。`json:"profile,omitempty"` に変えて `make swag` を回したところ
  `api/` の差分はゼロだった。swag の出力は `omitempty` の有無で変わらず、クライアントの生成型も
  変わらない。効果は JSON のバイト数だけだが、参加者一覧は定員でバウンドされ、`"profile":null,` は
  約 16 バイト、100 人でも 1.6KB にしかならない。

  2 つ目は**同一レスポンス内で表現が割れる**こと。「匿名参加」という 1 つの事実を、`profileId` は
  `null` で表すのに `profile` だけキー消滅で表すことになる。`profileId` は既存フィールドで、
  非ポインタ化も省略化も破壊的変更になるため揃えられない。

  3 つ目は**「キーが無い」が多義的**なこと。「この参加者にプロフィールが無い」とも「この API バージョンに
  まだ `profile` 項目が無い」とも読める。参加者一覧において「この人は匿名参加」は主催者にとって意味の
  ある情報なので、消すのではなく明示する側。

  ADR-0015 が一覧の `tags` で `omitempty` を選んだのは、要素数が多く空配列の重複が効き、かつタグの欠損が
  情報として重要でないケースで、本件には当てはまらない。この判断は本 ADR 限りにせず、
  [Development.md](../Development.md) の「レスポンス DTO の欠損表現」へ一般則として書き出した。

## 関連コミット

- ADR 作成: `docs: ADR作成 #162`
- 機能実装: `feat: 参加者一覧にプロフィールサマリーを追加 #162`
- テスト追加: `test: 参加者一覧のプロフィール取得テストを追加 #162`
- OpenAPI 更新: `docs: swagger 再生成 #162`
