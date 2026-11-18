package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

const contactMessageMaxLength = 2000

// ContactService は入力を検証し、固定の管理者宛メールを予約する（ADR-0052）。
type ContactService struct {
	repo      repository.ContactRepository
	contactTo string
}

// NewContactService は検証済みのCONTACT_TOを受け取る。
func NewContactService(repo repository.ContactRepository, contactTo string) *ContactService {
	return &ContactService{repo: repo, contactTo: contactTo}
}

// Submit はoutboxへの保存後に受付内容を返す。メール配信完了は待たない。
func (s *ContactService) Submit(ctx context.Context, req model.ContactRequest) (model.ContactResponse, error) {
	if !req.Category.IsValid() {
		return model.ContactResponse{}, &ValidationError{Message: "お問い合わせ内容を選択してください"}
	}
	message := strings.TrimSpace(req.Message)
	if utf8.RuneCountInString(message) > contactMessageMaxLength {
		return model.ContactResponse{}, &ValidationError{Message: "お問い合わせの本文は2000文字以内で入力してください"}
	}

	categoryNames := map[model.ContactCategory]string{
		model.ContactCategoryRequest: "要望",
		model.ContactCategoryBug:     "不具合報告",
		model.ContactCategoryOther:   "その他",
	}
	categoryName := categoryNames[req.Category]
	receivedAt := time.Now().UTC().Truncate(time.Microsecond)
	subject := "【お問い合わせ】" + categoryName
	body := fmt.Sprintf("カテゴリ: %s\n受付日時: %s\n\nお問い合わせ内容:\n%s",
		categoryName, receivedAt.Format(time.RFC3339Nano), message)
	if err := s.repo.Enqueue(ctx, s.contactTo, subject, body, receivedAt); err != nil {
		return model.ContactResponse{}, fmt.Errorf("submit contact: %w", err)
	}
	return model.ContactResponse{Category: req.Category, Message: message, ReceivedAt: receivedAt}, nil
}
