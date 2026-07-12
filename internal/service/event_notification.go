package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	subject, body, err := validateNotificationContent(req.Subject, req.Body)
	if err != nil {
		return model.SendEventNotificationResponse{}, err
	}

	parsedEventID, err := requireEventOwner(ctx, s.eventRepo, profileID, eventID)
	if err != nil {
		return model.SendEventNotificationResponse{}, err
	}

	recipients, err := s.joinRepo.ListRecipients(ctx, parsedEventID)
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
		EventID:        parsedEventID.String(),
		RecipientCount: len(recipients),
		SentCount:      len(recipients),
		FailedCount:    0,
	}, nil
}

// validateNotificationContent は通知メールの件名・本文を検証し、trim 済みの値を返す。
// 問題があれば *ValidationError を返す。件名・本文とも必須（空文字は不可）。
//
// イベント参加者への一斉送信（SendBulk）から呼ばれる。イベントキャンセル通知
// （EventCommandService.Cancel）は件名・本文が任意のため、代わりに
// validateOptionalNotificationContent を使う。
func validateNotificationContent(rawSubject, rawBody string) (subject, body string, err error) {
	subject = strings.TrimSpace(rawSubject)
	if subject == "" {
		return "", "", &ValidationError{Message: "件名は必須です"}
	}
	if len([]rune(subject)) > notificationSubjectMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("件名は%d文字以内で入力してください", notificationSubjectMaxLen)}
	}

	body = strings.TrimSpace(rawBody)
	if body == "" {
		return "", "", &ValidationError{Message: "本文は必須です"}
	}
	if len([]rune(body)) > notificationBodyMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("本文は%d文字以内で入力してください", notificationBodyMaxLen)}
	}

	return subject, body, nil
}

// validateOptionalNotificationContent は通知メールの件名・本文を検証し、trim 済みの値を返す。
// validateNotificationContent と異なり、空文字（未指定）はエラーとせずそのまま許容する。
// 指定されている場合のみ最大文字数を検証し、超過していれば *ValidationError を返す。
//
// イベントキャンセル通知（EventCommandService.Cancel）で使う。空の場合の既定文面への
// 補完はこの関数の責務ではなく、呼び出し元（EventCommandService.Cancel）が行う。
func validateOptionalNotificationContent(rawSubject, rawBody string) (subject, body string, err error) {
	subject = strings.TrimSpace(rawSubject)
	if len([]rune(subject)) > notificationSubjectMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("件名は%d文字以内で入力してください", notificationSubjectMaxLen)}
	}

	body = strings.TrimSpace(rawBody)
	if len([]rune(body)) > notificationBodyMaxLen {
		return "", "", &ValidationError{Message: fmt.Sprintf("本文は%d文字以内で入力してください", notificationBodyMaxLen)}
	}

	return subject, body, nil
}
