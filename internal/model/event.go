package model

import (
	"time"

	"github.com/google/uuid"
)

// EventCostInput はイベント費用の入力 DTO（カテゴリと金額）。
type EventCostInput struct {
	// Category は費用カテゴリ（例: "参加費"）。
	Category string `json:"category" validate:"required,max=255"`
	// Cost は費用（円）。0 以上の整数。
	Cost int `json:"cost" validate:"min=0"`
}

// EventItemInput はイベント持ち物の入力 DTO。
type EventItemInput struct {
	// Item は持ち物名（例: "双眼鏡"）。
	Item string `json:"item" validate:"required,max=255"`
	// IsRequired は必須かどうか。
	IsRequired bool `json:"isRequired"`
}

// CreateEventRequest はイベント投稿エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント投稿に必要な情報。
type CreateEventRequest struct {
	// Title はイベントタイトル（必須・255文字以内）。
	Title string `json:"title" example:"サクラ観察会" validate:"required,max=255"`
	// Description はイベント説明（必須・10,000文字以内）。
	Description string `json:"description" example:"春の桜を観察するイベントです。" validate:"required,max=10000"`
	// Location は開催場所（必須・255文字以内）。
	Location string `json:"location" example:"東京都新宿御苑" validate:"required,max=255"`
	// EventDate はイベント開催日時(RFC3339)（必須）。
	EventDate time.Time `json:"eventDate" example:"2026-07-01T10:00:00Z" validate:"required"`
	// EndDate はイベント終了日時(RFC3339)（任意）。省略時は EventDate と同値を補完する。
	EndDate time.Time `json:"endDate" example:"2026-07-01T17:00:00Z" validate:"omitempty"`
	// ApplicationDeadline は申込期限(RFC3339)（任意）。省略時は期限なし。終了日時以前であること。
	ApplicationDeadline time.Time `json:"applicationDeadline" example:"2026-06-25T23:59:59Z" validate:"omitempty"`
	// Capacity は定員（任意・0=未設定・正数=定員）。
	Capacity int `json:"capacity,omitempty" example:"30" validate:"min=0"`
	// ExternalURL は関連URLs（任意・255文字以内・http/https）。
	ExternalURL string `json:"externalUrl,omitempty" example:"https://example.com/event" validate:"omitempty,max=255"`
	// Costs は費用内訳（1件以上必須）。
	Costs []EventCostInput `json:"costs" validate:"required,min=1,dive"`
	// Items は持ち物リスト（任意）。
	Items []EventItemInput `json:"items,omitempty" validate:"omitempty,dive"`
	// ImageObjectKeys は画像オブジェクトキーの一覧（任意）。
	ImageObjectKeys []string `json:"imageObjectKeys,omitempty" validate:"omitempty,dive"`
	// PdfObjectKeys はPDFオブジェクトキーの一覧（任意・各要素255文字以内）。
	PdfObjectKeys []string `json:"pdfObjectKeys,omitempty" validate:"omitempty,dive,max=255"`
	// ImageFilenames は画像の元ファイル名一覧（任意）。指定時は ImageObjectKeys と同数・同順。
	// ダウンロード時のファイル名（Content-Disposition）と UI 表示に使う。
	ImageFilenames []string `json:"imageFilenames,omitempty" validate:"omitempty,dive,max=255"`
	// PdfFilenames はPDFの元ファイル名一覧（任意）。指定時は PdfObjectKeys と同数・同順。
	PdfFilenames []string `json:"pdfFilenames,omitempty" validate:"omitempty,dive,max=255"`
	// TagIDs は紐づけるタグの UUID 一覧（任意）。
	TagIDs []string `json:"tagIds,omitempty" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890" validate:"omitempty,dive"`
}

// NewEvent は検証済みのイベントドメイン型。repository 層に渡す。
type NewEvent struct {
	ProfileID   string
	Title       string
	Description string
	Location    string
	EventDate   time.Time
	// EndDate は補完・検証済みの終了日時（必ず EventDate 以上）。
	EndDate time.Time
	// ApplicationDeadline は検証済みの申込期限。ゼロ値は期限なし（DB では NULL）。
	ApplicationDeadline time.Time
	Capacity            int
	ExternalURL         string
	Costs               []EventCostInput
	Items               []EventItemInput
	ImageObjectKeys     []string
	PdfObjectKeys       []string
	// ImageFilenames は ImageObjectKeys と同順の元ファイル名（未指定は空文字）。
	ImageFilenames []string
	// PdfFilenames は PdfObjectKeys と同順の元ファイル名（未指定は空文字）。
	PdfFilenames []string
	// TagIDs は紐づけるタグの UUID 一覧（trim・重複除去済み）。
	TagIDs []string
}

// EventStatus はイベント一覧の開催状況絞り込みで使う値（ADR-0027）。
type EventStatus string

const (
	// EventStatusUpcoming は開催前（event_date > now()）。
	EventStatusUpcoming EventStatus = "upcoming"
	// EventStatusOngoing は開催中（event_date <= now() かつ end_date >= now()）。
	EventStatusOngoing EventStatus = "ongoing"
	// EventStatusEnded は開催後（end_date < now()）。
	EventStatusEnded EventStatus = "ended"
)

// IsValid は定義済みの開催状況かどうかを返す。
func (s EventStatus) IsValid() bool {
	switch s {
	case EventStatusUpcoming, EventStatusOngoing, EventStatusEnded:
		return true
	default:
		return false
	}
}

// EventSearchFilter はイベント一覧の絞り込み条件をまとめた検証済みの内部型。
// service 層で正規化してから repository 層へ渡す（HTTP には露出しない）。
//
// 条件を増やす際はフィールドを追加する。引数を並べる形にすると同型（[]string や string）が
// 並んで取り違えてもコンパイルが通らないため、構造体に集約している。
type EventSearchFilter struct {
	// Keywords は AND 検索するキーワード（trim・空要素除去済み）。
	// 各キーワードは title/description/主催者名/location/持ち物 を横断（OR）する。
	Keywords []string
	// TagIDs は絞り込み対象のタグ UUID（正準形・重複除去済み）。
	// 複数指定時は OR（いずれかのタグを持つイベントが該当する）。
	TagIDs []string
	// Statuses は絞り込み対象の開催状況（重複除去済み・定義順に整列済み）。
	// 複数指定時は OR（いずれかの状況に該当するイベントが該当する）。
	Statuses []EventStatus
	// Locations は絞り込み対象の地域文字列（trim・空要素除去・重複除去済み）。
	// 複数指定時は OR（いずれかに部分一致するイベントが該当する）（ADR-0028）。
	Locations []string
}

// IsEmpty は絞り込み条件が 1 つも無いことを返す。
// true の場合、呼び出し元は検索ではなく全件一覧の経路を使う。
func (f EventSearchFilter) IsEmpty() bool {
	return len(f.Keywords) == 0 && len(f.TagIDs) == 0 && len(f.Statuses) == 0 && len(f.Locations) == 0
}

// CreateEventResponse はイベント投稿エンドポイントのレスポンス DTO。
type CreateEventResponse struct {
	// ID は生成されたイベントの UUID。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-23T12:00:00Z"`
}

// EventSummary はイベント一覧で返す DTO（詳細フィールドは含まない）。
type EventSummary struct {
	// ID はイベントの一意識別子(UUID)。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID は投稿者のプロフィール ID(UUID)。
	ProfileID string `json:"profileId" example:"d290f1ee-6c54-4b01-90e6-d701748f0851"`
	// Title はイベントタイトル。
	Title string `json:"title" example:"サクラ観察会"`
	// Location は開催場所（文字列）。
	Location string `json:"location" example:"東京都新宿御苑"`
	// EventDate はイベント開催日時(RFC3339)。
	EventDate time.Time `json:"eventDate" example:"2026-07-01T10:00:00Z"`
	// EndDate はイベント終了日時(RFC3339)（任意）。省略時は EventDate と同値を補完する。
	EndDate time.Time `json:"endDate" example:"2026-07-01T17:00:00Z"`
	// ApplicationDeadline は申込期限(RFC3339)。nil の場合は期限なし（ADR-0029）。
	ApplicationDeadline *time.Time `json:"applicationDeadline,omitempty" example:"2026-06-25T23:59:59Z"`
	// CancelledAt はイベントが取りやめになった日時(RFC3339)。nil の場合は開催予定。
	CancelledAt *time.Time `json:"cancelledAt,omitempty" example:"2026-06-25T10:00:00Z"`
	// CreatedAt はレコード作成日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-06-22T12:00:00Z"`
	// ProfileSummary は投稿者プロフィールのサマリー情報。
	Profile ProfileSummary `json:"profile"`
	// Tags は紐づくタグの一覧（name 昇順）。タグが無い場合は省略される。
	Tags []TagResponse `json:"tags,omitempty"`
}

// EventListResponse はイベント一覧取得エンドポイントのレスポンス型。
//
// swag 用注釈のためにラッパー型を定義する。
type EventListResponse struct {
	// Events はイベントサマリーの一覧。
	Events []EventSummary `json:"events"`
	// TotalCount は現在の絞り込み条件（q / tagId / status / location）に一致する総件数。
	// 条件を指定しない場合は全件数になる。クライアントが最終ページ offset を算出するために使う。
	TotalCount int `json:"totalCount" example:"153"`
	// Limit は正規化後の実際に使われた取得件数。
	Limit int `json:"limit" example:"20"`
	// Offset は正規化後の実際に使われた取得開始位置。
	Offset int `json:"offset" example:"0"`
}

// EventResponse はイベント詳細取得エンドポイントのレスポンス型。
type EventResponse struct {
	ID          string         `json:"id"`
	Profile     ProfileSummary `json:"profile"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Location    string         `json:"location"`
	EventDate   time.Time      `json:"eventDate"`
	EndDate     time.Time      `json:"endDate"`
	// ApplicationDeadline は申込期限(RFC3339)。nil の場合は期限なし（ADR-0029）。
	ApplicationDeadline *time.Time `json:"applicationDeadline,omitempty"`
	Capacity            int        `json:"capacity"`
	// ParticipantCount は現在申込中の参加人数の合計（各申込の partySize の合計）。
	// 定員未設定（capacity=0）でも値を返す。定員がある場合の残り人数は
	// capacity - participantCount で算出する（ADR-0024）。
	ParticipantCount int                 `json:"participantCount" example:"20"`
	ExternalURL      string              `json:"externalUrl"`
	Costs            []EventCostResponse `json:"costs"`
	Items            []EventItemResponse `json:"items"`
	ImageObjectKeys  []string            `json:"imageObjectKeys"`
	PdfObjectKeys    []string            `json:"pdfObjectKeys"`
	// ImageFilenames は ImageObjectKeys に対応する元ファイル名（未設定は空文字）。
	ImageFilenames []string `json:"imageFilenames"`
	// PdfFilenames は PdfObjectKeys に対応する元ファイル名（未設定は空文字）。
	PdfFilenames []string `json:"pdfFilenames"`
	// ImageUrls は ImageObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	ImageUrls []string `json:"imageUrls"`
	// PdfUrls は PdfObjectKeys に対応する表示用の完全URL。
	// 公開ベースURL（R2_PUBLIC_BASE_URL）未設定時は空配列。
	PdfUrls []string `json:"pdfUrls"`
	// Tags は紐づくタグの一覧（name 昇順）。
	Tags      []TagResponse `json:"tags"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	// CancelledAt はイベントが取りやめになった日時(RFC3339)。nil の場合は開催予定。
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
}

// EventCostResponse はイベント費用のレスポンス DTO。
type EventCostResponse struct {
	Category string `json:"category"`
	Cost     int    `json:"cost"`
}

// EventItemResponse はイベント持ち物のレスポンス DTO。
type EventItemResponse struct {
	Item       string `json:"item"`
	IsRequired bool   `json:"isRequired"`
}

// CancelEventRequest はイベントキャンセルエンドポイントのリクエストボディ DTO。
//
//	@Description	イベント取りやめ確定時に参加者へ送る通知の件名・本文。同一トランザクションで
//	@Description	outbox に予約され、バックグラウンドワーカーが参加者へ個別送信する。
//	@Description	件名・本文はいずれも任意で、省略（空文字）時はサーバーが既定文面を補って予約する。
type CancelEventRequest struct {
	// Subject は参加者へ送る通知メールの件名（任意・255文字以内）。
	// 省略（空文字）時はサーバーが既定文面を補う。
	Subject string `json:"subject" example:"【重要】イベント開催中止のお知らせ" validate:"max=255"`
	// Body は参加者へ送る通知メールの本文（任意・10,000文字以内）。
	// 省略（空文字）時はサーバーが既定文面を補う。
	Body string `json:"body" example:"台風接近に伴い、安全のため本イベントは中止とさせていただきます。" validate:"max=10000"`
}

// CancelEventResponse はイベントキャンセルエンドポイントのレスポンス DTO。
type CancelEventResponse struct {
	// ID はキャンセルしたイベントの UUID。
	ID string `json:"id" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// CancelledAt はキャンセル日時(RFC3339)。
	CancelledAt time.Time `json:"cancelledAt" example:"2026-06-25T10:00:00Z"`
}

// MyEventKind はプロフィール単位で取得するイベントの種別。
type MyEventKind string

const (
	// MyEventKindHosted は自分が主催したイベント（過去・未来・キャンセル済みを含む）。
	MyEventKindHosted MyEventKind = "hosted"
	// MyEventKindApplied は申し込み済みで、終了日時(end_date)が未到来のイベント。
	MyEventKindApplied MyEventKind = "applied"
	// MyEventKindAttended は申し込み済みで、終了日時(end_date)を過ぎたイベント。
	MyEventKindAttended MyEventKind = "attended"
)

// IsValid は定義済みの種別かどうかを返す。
func (k MyEventKind) IsValid() bool {
	switch k {
	case MyEventKindHosted, MyEventKindApplied, MyEventKindAttended:
		return true
	default:
		return false
	}
}

// MyEventFilter はプロフィール単位のイベント一覧の絞り込み条件をまとめた検証済みの内部型。
// service 層で検証してから repository 層へ渡す（HTTP には露出しない）。
type MyEventFilter struct {
	// ProfileID は対象プロフィールの UUID（handler 層でパース済み・ADR-0010）。
	ProfileID uuid.UUID
	// Kind は取得する種別。IsValid() が true であることを前提とする。
	Kind MyEventKind
}

// MyEventCounts は種別ごとのイベント件数。マイページのタブ表示に使う。
type MyEventCounts struct {
	// Hosted は主催したイベントの件数。
	Hosted int `json:"hosted" example:"4"`
	// Applied は申し込み中のイベントの件数。
	Applied int `json:"applied" example:"2"`
	// Attended は参加済みのイベントの件数。
	Attended int `json:"attended" example:"3"`
}

// Of は指定した種別の件数を返す。未知の種別は 0 を返す。
func (c MyEventCounts) Of(kind MyEventKind) int {
	switch kind {
	case MyEventKindHosted:
		return c.Hosted
	case MyEventKindApplied:
		return c.Applied
	case MyEventKindAttended:
		return c.Attended
	default:
		return 0
	}
}

// MyEventListResponse はマイページのイベント一覧取得エンドポイントのレスポンス型。
//
//	@Description	指定種別のイベント一覧と、3種別すべての件数。
type MyEventListResponse struct {
	// Events はイベントサマリーの一覧（種別は counts ではなくリクエストの type に対応する）。
	Events []EventSummary `json:"events"`
	// Counts は3種別すべての件数。タブのバッジ表示に使う（1回のリクエストで揃う）。
	Counts MyEventCounts `json:"counts"`
	// TotalCount はリクエストした種別の総件数（counts の該当値と一致する）。
	TotalCount int `json:"totalCount" example:"2"`
	// Limit は正規化後の実際に使われた取得件数。
	Limit int `json:"limit" example:"20"`
	// Offset は正規化後の実際に使われた取得開始位置。
	Offset int `json:"offset" example:"0"`
}

// IsPublic は他人のプロフィールで公開してよい種別かどうかを返す。
// applied（申し込み中）は本人限定のため false になる。公開範囲の判断は ADR-0025 を参照。
func (k MyEventKind) IsPublic() bool {
	switch k {
	case MyEventKindHosted, MyEventKindAttended:
		return true
	default:
		return false
	}
}

// ProfileEventCounts はプロフィールページで公開する種別ごとのイベント件数。
// 申し込み中（applied）は本人限定のため含めない。
type ProfileEventCounts struct {
	// Hosted は主催したイベントの件数。
	Hosted int `json:"hosted" example:"4"`
	// Attended は参加済みのイベントの件数。
	Attended int `json:"attended" example:"3"`
}

// Of は指定した種別の件数を返す。公開対象外の種別は 0 を返す。
func (c ProfileEventCounts) Of(kind MyEventKind) int {
	switch kind {
	case MyEventKindHosted:
		return c.Hosted
	case MyEventKindAttended:
		return c.Attended
	default:
		return 0
	}
}

// ProfileEventListResponse はプロフィールのイベント一覧取得エンドポイントのレスポンス型。
//
//	@Description	指定種別のイベント一覧と、公開する2種別の件数。
type ProfileEventListResponse struct {
	// Events はイベントサマリーの一覧（リクエストの type に対応する）。
	Events []EventSummary `json:"events"`
	// Counts は公開する2種別の件数。タブのバッジ表示に使う（1回のリクエストで揃う）。
	Counts ProfileEventCounts `json:"counts"`
	// TotalCount はリクエストした種別の総件数（counts の該当値と一致する）。
	TotalCount int `json:"totalCount" example:"3"`
	// Limit は正規化後の実際に使われた取得件数。
	Limit int `json:"limit" example:"20"`
	// Offset は正規化後の実際に使われた取得開始位置。
	Offset int `json:"offset" example:"0"`
}
