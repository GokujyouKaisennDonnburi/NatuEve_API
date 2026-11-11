package model

import (
	"time"

	"github.com/google/uuid"
)

// イベント通知 outbox（event_notification_outbox）の status 値。
//
// pending: 未送信で送信待ち / sent: 送信済み（または宛先0件で送信対象なし）/
// failed: 最大試行回数に達し送信を諦めた状態。
const (
	NotificationOutboxStatusPending = "pending"
	NotificationOutboxStatusSent    = "sent"
	NotificationOutboxStatusFailed  = "failed"
)

// イベント通知 outbox（event_notification_outbox）の recipient_kind 値（ADR-0031）。
//
// members: 従来の参加者全員宛（event_members から宛先を解決）/
// organizer: 主催者1人宛（events JOIN profiles の email から宛先を解決）/
// email: recipient_email の1人宛（参加申込の確認メール・案内メール。ADR-0044）。
const (
	NotificationOutboxRecipientKindMembers   = "members"
	NotificationOutboxRecipientKindOrganizer = "organizer"
	NotificationOutboxRecipientKindEmail     = "email"
)

// EventNotificationOutbox は event_notification_outbox テーブルと対応するドメインモデル。
//
// Transactional Outbox パターンにより、イベントキャンセル確定や参加申込と同一トランザクションで
// この行を INSERT する（internal/repository の CancelWithNotification・Join 参照）。
// members / organizer の宛先はスナップショットせず、NotificationOutboxWorker が送信直前に
// event_members または events から解決する。email は RecipientEmail を使う（ADR-0044）。
type EventNotificationOutbox struct {
	ID      uuid.UUID
	EventID uuid.UUID
	// RecipientKind は宛先種別。NotificationOutboxRecipientKindMembers /
	// NotificationOutboxRecipientKindOrganizer / NotificationOutboxRecipientKindEmail のいずれか。
	RecipientKind string
	// RecipientEmail は recipient_kind='email' のときの宛先メールアドレス。
	// それ以外の種別では空文字。
	RecipientEmail string
	Subject        string
	Body           string
	// Status は NotificationOutboxStatusPending / Sent / Failed のいずれか。
	Status string
	// Attempts は送信を試みた回数。
	Attempts int
	// NextAttemptAt は次に送信を試みるべき日時。
	NextAttemptAt time.Time
	// LastError は直近の送信失敗時のエラー内容。未設定時は空文字。
	LastError string
	// SentAt は送信完了日時。未送信の場合は nil。
	SentAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OutboxProcessResponse は内部エンドポイント POST /internal/outbox/process のレスポンス。
type OutboxProcessResponse struct {
	// Processed は 1 回の呼び出しで処理した outbox 行の件数。
	Processed int `json:"processed"`
}
