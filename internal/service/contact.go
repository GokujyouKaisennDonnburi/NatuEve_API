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

// お問い合わせ全体の受付上限（ADR-0052）。送信済み・失敗を含む受付件数で数える。
const (
	contactAdmissionLimit  = 10
	contactAdmissionWindow = 24 * time.Hour
)

// ErrContactLimitExceeded はお問い合わせの受付上限超過。handlerはHTTP 429を返す。
var ErrContactLimitExceeded = repository.ErrContactLimitReached

var contactMailLocation = time.FixedZone("JST", 9*60*60)

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

	categoryName := req.Category.Label()
	receivedAt := time.Now().UTC().Truncate(time.Microsecond)
	subject := "【お問い合わせ】" + categoryName
	body := fmt.Sprintf("カテゴリ: %s\n受付日時: %s\n\nお問い合わせ内容:\n%s",
		categoryName, receivedAt.In(contactMailLocation).Format("2006-01-02 15:04:05 MST"), message)
	if err := s.repo.Enqueue(ctx, s.contactTo, subject, body, receivedAt, contactAdmissionWindow, contactAdmissionLimit); err != nil {
		return model.ContactResponse{}, fmt.Errorf("submit contact: %w", err)
	}
	return model.ContactResponse{Category: req.Category, Message: message, ReceivedAt: receivedAt}, nil
}
