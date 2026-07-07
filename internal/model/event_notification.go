package model

// SendEventNotificationRequest はイベント参加者への一斉送信エンドポイントのリクエストボディ DTO。
//
//	@Description	イベント参加者への一斉送信に必要な件名・本文。
type SendEventNotificationRequest struct {
	// Subject はメールの件名（必須・255文字以内）。
	Subject string `json:"subject" example:"【重要】明日のイベントは雨天決行です" validate:"required,max=255"`
	// Body はメールの本文（必須・10,000文字以内）。
	Body string `json:"body" example:"明日のイベントは予報通り雨となりますが、予定通り開催します。動きやすい服装でお越しください。" validate:"required,max=10000"`
}

// SendEventNotificationResponse はイベント参加者への一斉送信エンドポイントのレスポンス DTO。
type SendEventNotificationResponse struct {
	// EventID は送信対象のイベントID。
	EventID string `json:"eventId" example:"a1b2c3d4-e5f6-7890-abcd-ef1234567890"`
	// RecipientCount は送信対象の参加者数。
	RecipientCount int `json:"recipientCount" example:"12"`
	// SentCount は送信に成功した件数。
	SentCount int `json:"sentCount" example:"12"`
	// FailedCount は送信に失敗した件数。
	FailedCount int `json:"failedCount" example:"0"`
}
