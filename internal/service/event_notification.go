package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// Email は送信する1通分のメールを表す。
//
// ADR-0004（一斉送信は個別送信で行う）に従い、To は必ず単一宛先とする。
// 複数宛先を1通の To/BCC に詰めてはならない。
type Email struct {
	To      string
	Subject string
	Text    string
}

// Mailer はメール送信基盤（Resend など）を抽象化するインターフェイス。
// service 層はこのインターフェイス経由でメール送信を行う。
type Mailer interface {
	// SendBatch は emails を送信する。ADR-0004 に従い、実装側でも
	// 受信者ごとに個別のメールとして送信すること（To に複数宛先を入れない）。
	SendBatch(ctx context.Context, emails []Email) error
}

// ErrMailRateLimited はメール送信基盤のレート制限により送信できなかったことを表す。
//
// Mailer 実装がリトライしても解消しなかった場合に、これをラップして返す。
// handler 層は errors.Is で判定し HTTP 429 を返す。
var ErrMailRateLimited = errors.New("mail rate limited")

// maxNotificationRecipients は一斉送信1回あたりの宛先数上限。
//
// ADR-0001 で採用した Resend 無料枠は 100通/日のため、1回の一斉送信でこれを
// 使い切らないようにするガード（他の送信用途との共存を考慮した安全策）。
//
// 注意: この値は mail.ResendClient のバッチ上限（resendBatchMaxSize=100）と
// 一致しており、現状は SendBatch のチャンクが常に1回で収まる。この上限を
// 100 より大きくするとチャンクが複数回に分かれ、途中チャンクでの送信失敗時に
// SentCount/FailedCount が実態とずれる（前半は送信済みだが呼び出し元は 500 を返す）。
// 引き上げる際は SendBatch 側のチャンク単位の成否集計もあわせて見直すこと。
const maxNotificationRecipients = 100

// notificationSubjectMaxLen はメール件名の最大文字数。
const notificationSubjectMaxLen = 255

// notificationBodyMaxLen はメール本文の最大文字数。
const notificationBodyMaxLen = 10000

// EventNotificationService はイベント参加者への一斉送信のビジネスロジックを提供する。
type EventNotificationService struct {
	eventRepo repository.EventRepository
	joinRepo  repository.EventJoinRepository
	mailer    Mailer
}

// NewEventNotificationService は EventNotificationService を生成する。
func NewEventNotificationService(
	eventRepo repository.EventRepository,
	joinRepo repository.EventJoinRepository,
	mailer Mailer,
) *EventNotificationService {
	return &EventNotificationService{
		eventRepo: eventRepo,
		joinRepo:  joinRepo,
		mailer:    mailer,
	}
}

// SendBulk はイベント主催者からの依頼で、参加者全員へ一斉送信する。
//
// 送信できるのはイベント主催者のみ（events.profile_id == profileID）。
// 検証エラーは *ValidationError、認可エラーは *ForbiddenError として返す。
// handler 層で errors.As により判定する。
func (s *EventNotificationService) SendBulk(
	ctx context.Context,
	profileID, eventID string,
	req model.SendEventNotificationRequest,
) (model.SendEventNotificationResponse, error) {
	subject, body, err := validateSendEventNotificationRequest(req)
	if err != nil {
		return model.SendEventNotificationResponse{}, err
	}

	trimmedEventID := strings.TrimSpace(eventID)

	// 認可チェック: イベント主催者のみ一斉送信できる。
	ownerID, err := s.eventRepo.GetOwnerProfileID(ctx, trimmedEventID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SendEventNotificationResponse{}, &ValidationError{Message: "指定されたイベントが存在しません"}
	}
	if err != nil {
		return model.SendEventNotificationResponse{}, fmt.Errorf("get event owner: %w", err)
	}
	// UUID として正規化して比較する（大文字小文字・表記ゆれによる誤判定を避ける）。
	// パースに失敗した場合は認可を通さない（fail-closed）。
	ownerUID, ownerErr := uuid.Parse(ownerID)
	profileUID, profileErr := uuid.Parse(profileID)
	if ownerErr != nil || profileErr != nil || ownerUID != profileUID {
		return model.SendEventNotificationResponse{}, &ForbiddenError{Message: "このイベントの参加者へ通知を送信する権限がありません"}
	}

	recipients, err := s.joinRepo.ListRecipients(ctx, trimmedEventID)
	if err != nil {
		return model.SendEventNotificationResponse{}, fmt.Errorf("list recipients: %w", err)
	}
	if len(recipients) == 0 {
		return model.SendEventNotificationResponse{}, &ValidationError{Message: "送信先の参加者がいません"}
	}
	if len(recipients) > maxNotificationRecipients {
		return model.SendEventNotificationResponse{}, &ValidationError{
			Message: fmt.Sprintf("送信先の参加者が多すぎます（上限%d件）", maxNotificationRecipients),
		}
	}

	emails := make([]Email, 0, len(recipients))
	for _, recipient := range recipients {
		emails = append(emails, Email{
			To:      recipient.MailAddress,
			Subject: subject,
			Text:    body,
		})
	}

	if err := s.mailer.SendBatch(ctx, emails); err != nil {
		return model.SendEventNotificationResponse{}, fmt.Errorf("send batch: %w", err)
	}

	return model.SendEventNotificationResponse{
		EventID:        trimmedEventID,
		RecipientCount: len(recipients),
		SentCount:      len(recipients),
		FailedCount:    0,
	}, nil
}

// validateSendEventNotificationRequest はリクエストを検証し、trim 済みの subject/body を返す。
// 問題があれば *ValidationError を返す。
func validateSendEventNotificationRequest(req model.SendEventNotificationRequest) (subject, body string, err error) {
	subject = strings.TrimSpace(req.Subject)
	if subject == "" {
		return "", "", &ValidationError{Message: "件名は必須です"}
	}
	if len([]rune(subject)) > notificationSubjectMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("件名は%d文字以内で入力してください", notificationSubjectMaxLen)}
	}

	body = strings.TrimSpace(req.Body)
	if body == "" {
		return "", "", &ValidationError{Message: "本文は必須です"}
	}
	if len([]rune(body)) > notificationBodyMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("本文は%d文字以内で入力してください", notificationBodyMaxLen)}
	}

	return subject, body, nil
}
