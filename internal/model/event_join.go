package model

import (
	"time"

	"github.com/google/uuid"
)

// ParticipantInput は参加申込のカテゴリ別人数1件分の入力 DTO。
type ParticipantInput struct {
	// Category は参加者カテゴリ（必須・255文字以内）。
	// そのイベントの費用カテゴリ（costs[].category）に実在する名前を指定する。
	// 大文字小文字は区別しない。
	Category string `json:"category" example:"大人" validate:"required,max=255"`
	// HeadCount はそのカテゴリの人数（必須・1以上）。0人のカテゴリは送らない。
	HeadCount int `json:"headCount" example:"2" validate:"required,min=1"`
}

// ParticipantResponse は参加者のカテゴリ別人数1件分のレスポンス DTO。
type ParticipantResponse struct {
	// Category は参加者カテゴリ。
	Category string `json:"category" example:"大人"`
	// HeadCount はそのカテゴリの人数。
	HeadCount int `json:"headCount" example:"2"`
}

// JoinEventRequest はイベント参加申込エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント参加申込に必要な情報。認証は任意。
//	@Description	参加人数はカテゴリ別の内訳（participants）で送る。合計はサーバーが算出する。
type JoinEventRequest struct {
	// Username は参加するユーザーの表示名（必須・255文字以内）。
	Username string `json:"username" example:"山田太郎" validate:"required,max=255"`
	// MailAddress は参加するユーザーのメールアドレス（必須）。
	MailAddress string `json:"mailAddress" example:"yamada@example.com" validate:"required,email,max=255"`
	// Participants はカテゴリ別の参加人数（必須・1件以上）。
	// 同一カテゴリを複数の要素に分けて送ることはできない。
	Participants []ParticipantInput `json:"participants" validate:"required,min=1,dive"`
}

// JoinEventResponse は参加申込完了時に返すレスポンス。
type JoinEventResponse struct {
	// EventID は参加したイベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID は参加するユーザーのUUID。ログイン時のみ記録され、匿名参加時は null。
	ProfileID *uuid.UUID `json:"profileId" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// Username は参加するユーザーの表示名。
	Username string `json:"username" example:"山田太郎"`
	// MailAddress は参加するユーザーのメールアドレス。
	MailAddress string `json:"mailAddress" example:"yamada@example.com"`
	// PartySize は participants の合計人数（サーバーが算出した値）。
	PartySize int `json:"partySize" example:"3"`
	// Participants は登録されたカテゴリ別人数。イベントの費用カテゴリの登録順で返す。
	Participants []ParticipantResponse `json:"participants"`
	// CreatedAt は参加申込日時。
	CreatedAt time.Time `json:"createdAt" example:"2023-01-01T12:00:00Z"`
}

// MyEventApplicationResponse はログイン中ユーザー自身の申込内容取得エンドポイントのレスポンス DTO。
// 金額は含まない（ADR-0026）。参加費はイベント詳細（costs）を参照する。
type MyEventApplicationResponse struct {
	// EventID は申込先イベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// Username は申込時にフォームへ入力された名前。
	Username string `json:"username" example:"山田太郎"`
	// MailAddress は申込時に入力されたメールアドレス。
	MailAddress string `json:"mailAddress" example:"yamada@example.com"`
	// PartySize は代表者を含む参加人数。participants の合計と一致する。
	PartySize int `json:"partySize" example:"3"`
	// Participants はカテゴリ別人数の内訳（カテゴリ名の昇順）。
	// API 経由の申込では必ず1件以上入る。DB は内訳0件を禁じていないため、
	// 直接 INSERT された行では空配列になりうる（ADR-0026）。
	Participants []ParticipantResponse `json:"participants"`
	// CreatedAt は参加申込日時。
	CreatedAt time.Time `json:"createdAt" example:"2026-08-01T12:34:56Z"`
}

// LeaveEventResponse は参加キャンセル完了時に返すレスポンス。
//
//	@Description	参加キャンセルの結果。追記された参加状態ログ（action=leave）1件分の内容を返す。
type LeaveEventResponse struct {
	// EventID はキャンセルしたイベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID はキャンセルしたユーザーのUUID。leave は認証必須のため常に値が入る。
	ProfileID uuid.UUID `json:"profileId" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// Action は参加状態ログのアクション。常に "leave"。
	Action string `json:"action" example:"leave"`
	// CreatedAt は参加状態ログ（leave）の記録日時。
	CreatedAt time.Time `json:"createdAt" example:"2026-07-01T12:00:00Z"`
}

// AbsenceReason は欠席連絡の欠席理由（ADR-0031）。
type AbsenceReason string

const (
	// AbsenceReasonIllness は体調不良。
	AbsenceReasonIllness AbsenceReason = "illness"
	// AbsenceReasonFamily は家庭の都合。
	AbsenceReasonFamily AbsenceReason = "family"
	// AbsenceReasonWeatherTransport は天候・交通。
	AbsenceReasonWeatherTransport AbsenceReason = "weather_transport"
	// AbsenceReasonOther はその他。
	AbsenceReasonOther AbsenceReason = "other"
)

// IsValid は定義済みの欠席理由かどうかを返す。
func (r AbsenceReason) IsValid() bool {
	switch r {
	case AbsenceReasonIllness, AbsenceReasonFamily, AbsenceReasonWeatherTransport, AbsenceReasonOther:
		return true
	default:
		return false
	}
}

// AbsenceEventRequest はイベント欠席連絡エンドポイントのリクエストボディ DTO（ADR-0031）。
//
//	@Description	申込期限経過後の参加キャンセルに必要な情報。認証必須。
//	@Description	reason は任意（指定する場合は4値のいずれか）。detail は任意（trim 後200文字以内）。
type AbsenceEventRequest struct {
	// Reason は欠席理由（任意）。illness=体調不良 / family=家庭の都合 /
	// weather_transport=天候・交通 / other=その他。
	// フィールド省略・null・空文字はいずれも未指定として扱う。
	Reason AbsenceReason `json:"reason" example:"illness"`
	// Detail は欠席理由の詳細（任意・200文字以内）。
	Detail string `json:"detail" example:"熱が出たため"`
}

// AbsenceEventResponse は欠席連絡完了時に返すレスポンス。
//
//	@Description	欠席連絡の結果。追記された参加状態ログ（action=absence）1件分の内容を返す。
//	@Description	reason・detail は未指定で欠席連絡した場合 null。
//	@Description	主催者宛ての欠席連絡メールは outbox に予約され、非同期で送信される（レスポンスには含まれない）。
type AbsenceEventResponse struct {
	// EventID は欠席連絡したイベントのUUID。
	EventID uuid.UUID `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// ProfileID は欠席連絡したユーザーのUUID。absence は認証必須のため常に値が入る。
	ProfileID uuid.UUID `json:"profileId" example:"b2c3d4e5-f6a7-8901-bcde-f23456789012"`
	// Action は参加状態ログのアクション。常に "absence"。
	Action string `json:"action" example:"absence"`
	// Reason は記録された欠席理由。未指定時は null。
	Reason *string `json:"reason" example:"illness" extensions:"x-nullable"`
	// Detail は記録された欠席理由の詳細。未指定時は null。
	Detail *string `json:"detail" example:"熱が出たため" extensions:"x-nullable"`
	// CreatedAt は参加状態ログ（absence）の記録日時。
	CreatedAt time.Time `json:"createdAt" example:"2026-09-02T12:00:00Z"`
}

// EventRecipient はイベント参加者への一斉送信の宛先1件分を表すモデル。
// Repository 層で event_members から SELECT する際に使用する。
type EventRecipient struct {
	MailAddress string
}

// MemberCategory は event_member_categories の1行に対応するモデル。
// Repository 層で INSERT・SELECT する際に使用する。
type MemberCategory struct {
	// CostID は参照する event_costs の ID。カテゴリ名から repository が解決して埋める。
	CostID uuid.UUID
	// Category はカテゴリ名。
	Category string
	// HeadCount はそのカテゴリの人数（1以上）。
	HeadCount int
}

// EventMember は event_members テーブルと対応するモデル。
// Repository 層で INSERT・SELECT する際に使用する。
type EventMember struct {
	// ID は event_members の主キー。Join（INSERT）時に repository が採番して埋める。
	ID          uuid.UUID
	EventID     uuid.UUID
	ProfileID   uuid.NullUUID // ログイン時のみ Valid=true。匿名参加は Valid=false（DB上はNULL）。
	Username    string
	MailAddress string
	// PartySize は代表者を含む参加人数（1以上）。Categories の HeadCount 合計と一致する。
	PartySize int
	// Categories はカテゴリ別人数の内訳。Join（INSERT）時は Category と HeadCount を
	// 呼び出し元が埋め、CostID は repository がカテゴリ名から解決して埋める。
	Categories []MemberCategory
	CreatedAt  time.Time
	// Profile は profiles から LEFT JOIN で取得したプロフィールサマリー。
	// 匿名参加（ProfileID が Invalid）の場合は nil。Join（INSERT）経路では使わない。
	Profile *ProfileSummary
}

// EventMemberResponse は参加者一覧取得エンドポイントの1参加者分の DTO。
//
//	@Description	参加者1人分の情報。匿名参加かどうかは profile が null かどうかで判定できる。
//	@Description	profile は参加者のプロフィールサマリー。匿名参加の場合 null。
//	@Description	username は申込時に入力された名前で、profile.displayName（アカウントの表示名）とは別物。
type EventMemberResponse struct {
	// Username は申込時にフォームへ入力された名前。参加者が名乗った名前のスナップショットで、
	// 保存後は変更されない。アカウントの表示名（Profile.DisplayName）とは別物で、
	// ログイン参加でも一致するとは限らない。匿名参加でも必ず値が入る。
	Username string `json:"username" example:"山田太郎"`
	// PartySize は代表者を含む参加人数。participants の合計と一致する。
	PartySize int `json:"partySize" example:"3"`
	// Participants はカテゴリ別人数の内訳。イベントの費用カテゴリの登録順で返す。
	// 内訳を持たない参加者では空配列（null ではない）。
	Participants []ParticipantResponse `json:"participants"`
	// MailAddress は参加者のメールアドレス。
	MailAddress string `json:"mailAddress" example:"yamada@example.com"`
	// CreatedAt は参加申込日時(RFC3339)。
	CreatedAt time.Time `json:"createdAt" example:"2026-07-01T12:00:00Z"`
	// Profile は参加者のプロフィールサマリー（表示名・アイコン URL）。匿名参加の場合は null。
	// Username は申込時に入力された名前で、Profile.DisplayName（アカウントの表示名）とは別物。
	Profile *ProfileSummary `json:"profile" extensions:"x-nullable"`
}

// EventMemberListResponse は参加者一覧取得エンドポイントのレスポンス。
type EventMemberListResponse struct {
	// Members は参加者の一覧。0件の場合は空配列（null ではない）。
	Members []EventMemberResponse `json:"members"`
	// TotalCount は参加者総数（client の表示用）。
	TotalCount int `json:"totalCount" example:"5"`
	// TotalMembers は全参加者の partySize 合計（実際の総参加人数）。
	TotalMembers int `json:"totalMembers" example:"8"`
}
