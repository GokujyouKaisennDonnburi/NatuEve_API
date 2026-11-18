package model

import "time"

// ContactCategory はお問い合わせ内容の分類（ADR-0052）。
type ContactCategory string

// お問い合わせカテゴリとして受け付ける値。
const (
	ContactCategoryRequest ContactCategory = "request"
	ContactCategoryBug     ContactCategory = "bug"
	ContactCategoryOther   ContactCategory = "other"
)

// IsValid は定義された3種類のカテゴリかを返す。
func (c ContactCategory) IsValid() bool {
	switch c {
	case ContactCategoryRequest, ContactCategoryBug, ContactCategoryOther:
		return true
	default:
		return false
	}
}

// ContactRequest は認証不要のお問い合わせ受付リクエスト。
//
//	@Description	categoryは必須。messageは任意で、前後の空白を除いて2000文字以内。
type ContactRequest struct {
	Category ContactCategory `json:"category" binding:"required" enums:"request,bug,other" example:"request"`
	Message  string          `json:"message" example:"検索機能を追加してほしいです"`
}

// ContactResponse はメール配信ではなく受付完了を表す。
type ContactResponse struct {
	Category   ContactCategory `json:"category" enums:"request,bug,other" example:"request"`
	Message    string          `json:"message" example:"検索機能を追加してほしいです"`
	ReceivedAt time.Time       `json:"receivedAt" example:"2026-11-18T03:00:00Z"`
}
