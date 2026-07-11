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

// EventNotificationOutbox は event_notification_outbox テーブルと対応するドメインモデル。
//
// Transactional Outbox パターンにより、イベントキャンセル確定と同一トランザクションで
// この行を INSERT する（internal/repository の CancelWithNotification 参照）。
// 宛先はスナップショットせず、NotificationOutboxWorker が送信直前に
// event_members から解決する。
type EventNotificationOutbox struct {
	ID      uuid.UUID
	EventID uuid.UUID
	Subject string
	Body    string
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
