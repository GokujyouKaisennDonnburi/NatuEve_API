package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// NotFoundError はリソースが存在しないことを表す型付きエラー。
//
// handler 層が errors.As で判定し、HTTP 404 を返すために使う。
type NotFoundError struct {
	Message string
}

// Error は error インターフェイスを実装する。
func (e *NotFoundError) Error() string {
	return e.Message
}

// ConflictError はリソースの競合を表す型付きエラー。
//
// handler 層が errors.As で判定し、HTTP 409 を返すために使う。
// Code は機械可読なエラーコード。空なら handler 層が既定値 "conflict" を使う。
type ConflictError struct {
	Code    string
	Message string
}

// Error は error インターフェイスを実装する。
func (e *ConflictError) Error() string {
	return e.Message
}

// EventJoinService はイベント参加申込のビジネスロジックを担当する。
type EventJoinService struct {
	joinRepo  repository.EventJoinRepository
	eventRepo repository.EventRepository
	// wake は欠席連絡の通知予約後にバックグラウンドワーカーを起床させる。
	// nil 安全（未設定＝呼ばない）。NotificationOutboxWorker.Wake はメソッド自体も
	// nil レシーバ安全なため、ワーカー未生成環境（Resend 未設定）でも
	// worker.Wake をそのまま渡してよい。
	wake func()
}

// NewEventJoinService は Service を生成する。
// wake は nil でも可（未設定時は Absence 後の起床通知を行わない）。
func NewEventJoinService(joinRepo repository.EventJoinRepository, eventRepo repository.EventRepository, wake func()) *EventJoinService {
	return &EventJoinService{joinRepo: joinRepo, eventRepo: eventRepo, wake: wake}
}

// Join はイベント参加処理を行う。
//
// profileID が Invalid（匿名参加）の場合は profile_id を NULL として登録する。
// 存在確認・重複確認・定員確認・登録は repository が1トランザクションで
// 原子的に行い、結果は sentinel エラーで返るためここで HTTP 向けエラーに変換する。
func (s *EventJoinService) Join(
	ctx context.Context,
	eventID uuid.UUID,
	profileID uuid.NullUUID,
	req model.JoinEventRequest,
) (model.JoinEventResponse, error) {

	// バリデーション
	if err := validateJoinEventRequest(req); err != nil {
		return model.JoinEventResponse{}, err
	}

	// 内訳を組み立て、合計人数を算出する。
	// 合計はクライアントから受け取らず、必ず内訳から求めた値を保存する。
	categories := make([]model.MemberCategory, 0, len(req.Participants))
	partySize := 0
	for _, p := range req.Participants {
		categories = append(categories, model.MemberCategory{
			Category:  strings.TrimSpace(p.Category),
			HeadCount: p.HeadCount,
		})
		partySize += p.HeadCount
	}

	// 参加登録（バリデーション済みの値を使う）
	member := &model.EventMember{
		EventID:     eventID,
		ProfileID:   profileID,
		Username:    strings.TrimSpace(req.Username),
		MailAddress: strings.TrimSpace(req.MailAddress),
		PartySize:   partySize,
		Categories:  categories,
	}

	if err := s.joinRepo.Join(ctx, member); err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.JoinEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrAlreadyJoined):
			return model.JoinEventResponse{}, &ConflictError{Code: "already_joined", Message: "既に参加しています"}
		case errors.Is(err, repository.ErrEventCapacityFull):
			return model.JoinEventResponse{}, &ConflictError{Code: "capacity_full", Message: "定員に達しています"}
		case errors.Is(err, repository.ErrEventCancelled):
			return model.JoinEventResponse{}, &ConflictError{Code: "event_cancelled", Message: "このイベントはキャンセルされているため参加できません"}
		case errors.Is(err, repository.ErrDeadlinePassed):
			return model.JoinEventResponse{}, &ConflictError{Code: "deadline_passed", Message: "申込期限を過ぎているため申し込めません"}
		case errors.Is(err, repository.ErrCategoryNotFound):
			return model.JoinEventResponse{}, &ValidationError{Message: "指定された参加者カテゴリはこのイベントに存在しません"}
		case errors.Is(err, repository.ErrDuplicateCategory):
			return model.JoinEventResponse{}, &ValidationError{Message: "同じ参加者カテゴリが重複しています"}
		}
		return model.JoinEventResponse{}, fmt.Errorf("join event: %w", err)
	}

	// レスポンスの ProfileID: ログイン時のみ値を返す。匿名は nil（JSON: null）。
	var respProfileID *uuid.UUID
	if profileID.Valid {
		v := profileID.UUID
		respProfileID = &v
	}

	return model.JoinEventResponse{
		EventID:      member.EventID,
		ProfileID:    respProfileID,
		Username:     member.Username,
		MailAddress:  member.MailAddress,
		PartySize:    member.PartySize,
		Participants: toParticipantResponses(member.Categories),
		CreatedAt:    member.CreatedAt,
	}, nil
}

// toParticipantResponses は内訳をレスポンス DTO へ変換する。
// カテゴリ名の昇順に整列し、内訳がない場合も nil ではなく空スライスを返す
// （JSON で null ではなく [] にするため）。
func toParticipantResponses(categories []model.MemberCategory) []model.ParticipantResponse {
	participants := make([]model.ParticipantResponse, 0, len(categories))
	for _, c := range categories {
		participants = append(participants, model.ParticipantResponse{
			Category:  c.Category,
			HeadCount: c.HeadCount,
		})
	}
	sort.Slice(participants, func(i, j int) bool {
		return participants[i].Category < participants[j].Category
	})
	return participants
}

// Leave はログイン参加者のイベント参加を取り消す。
//
// 参加行の削除と参加状態ログ（action='leave'）の追記は repository が1トランザクションで
// 原子的に行い、結果は sentinel エラーで返るためここで HTTP 向けエラーに変換する。
// leave は認証必須のため profileID は常に有効値。匿名参加はこの経路の対象外。
func (s *EventJoinService) Leave(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.LeaveEventResponse, error) {

	createdAt, err := s.joinRepo.Leave(ctx, eventID, profileID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.LeaveEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrNotJoined):
			return model.LeaveEventResponse{}, &NotFoundError{Message: "このイベントに参加していません"}
		case errors.Is(err, repository.ErrDeadlinePassed):
			return model.LeaveEventResponse{}, &ConflictError{
				Code:    "deadline_passed",
				Message: "申込期限経過後のキャンセルは欠席連絡 API を利用してください",
			}
		}
		return model.LeaveEventResponse{}, fmt.Errorf("leave event: %w", err)
	}

	return model.LeaveEventResponse{
		EventID:   eventID,
		ProfileID: profileID,
		Action:    "leave",
		CreatedAt: createdAt,
	}, nil
}

// absenceDetailMaxLen は欠席理由の詳細（detail）の最大文字数（ADR-0031）。
const absenceDetailMaxLen = 200

// 欠席連絡メールの件名プレフィックス・サフィックス。件名は
// 「【欠席連絡】「{title}」への参加キャンセル」という形式で組み立てる。
const (
	absenceSubjectPrefix = "【欠席連絡】「"
	absenceSubjectSuffix = "」への参加キャンセル"
)

// Absence はログイン参加者の欠席連絡を受け付ける（ADR-0031）。
//
// 参加行の削除・参加状態ログ（action='absence'）の追記・主催者宛通知の outbox 予約は
// repository が1トランザクションで原子的に行い、結果は sentinel エラーで返るため
// ここで HTTP 向けエラーに変換する。
// メール文面はサーバーが組み立て、クライアントからは送らない。
// leave は認証必須のため profileID は常に有効値。匿名参加はこの経路の対象外。
func (s *EventJoinService) Absence(
	ctx context.Context,
	eventID, profileID uuid.UUID,
	req model.AbsenceEventRequest,
) (model.AbsenceEventResponse, error) {

	// バリデーション
	if err := validateAbsenceEventRequest(req); err != nil {
		return model.AbsenceEventResponse{}, err
	}

	// メール文面に使う参加者名を取得する。未参加は Leave と同じ文言の NotFoundError。
	member, err := s.joinRepo.GetMemberByProfile(ctx, eventID, profileID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.AbsenceEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrNotJoined):
			return model.AbsenceEventResponse{}, &NotFoundError{Message: "このイベントに参加していません"}
		}
		return model.AbsenceEventResponse{}, fmt.Errorf("get member by profile: %w", err)
	}

	// 件名に載せるイベントタイトルを取得する。GetMemberByProfile 通過後も
	// イベント削除と競合しうるため ErrEventNotFound は NotFoundError に変換する。
	title, err := s.eventRepo.GetTitle(ctx, eventID.String())
	if err != nil {
		if errors.Is(err, repository.ErrEventNotFound) {
			return model.AbsenceEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		}
		return model.AbsenceEventResponse{}, fmt.Errorf("get event title: %w", err)
	}

	detail := strings.TrimSpace(req.Detail)
	subject, body := buildAbsenceNotification(title, member.Username, req.Reason, detail)

	createdAt, err := s.joinRepo.Absence(ctx, eventID, profileID, string(req.Reason), detail, subject, body)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.AbsenceEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrNotJoined):
			return model.AbsenceEventResponse{}, &NotFoundError{Message: "このイベントに参加していません"}
		case errors.Is(err, repository.ErrAbsenceBeforeDeadline):
			return model.AbsenceEventResponse{}, &ConflictError{
				Code:    "before_deadline",
				Message: "申込期限前に欠席連絡はできません。参加キャンセル API を利用してください",
			}
		case errors.Is(err, repository.ErrEventEnded):
			return model.AbsenceEventResponse{}, &ConflictError{
				Code:    "event_ended",
				Message: "イベントは終了しているため欠席連絡できません",
			}
		case errors.Is(err, repository.ErrEventCancelled):
			return model.AbsenceEventResponse{}, &ConflictError{
				Code:    "event_cancelled",
				Message: "このイベントはキャンセルされているため欠席連絡できません",
			}
		}
		return model.AbsenceEventResponse{}, fmt.Errorf("absence event: %w", err)
	}

	if s.wake != nil {
		s.wake()
	}

	// 未指定の reason・detail は DB の NULL と揃えてレスポンスでも null で返す。
	var reasonResp *string
	if req.Reason != "" {
		r := string(req.Reason)
		reasonResp = &r
	}
	var detailResp *string
	if detail != "" {
		d := detail
		detailResp = &d
	}

	return model.AbsenceEventResponse{
		EventID:   eventID,
		ProfileID: profileID,
		Action:    "absence",
		Reason:    reasonResp,
		Detail:    detailResp,
		CreatedAt: createdAt,
	}, nil
}

// validateAbsenceEventRequest はリクエストの各フィールドを検証する。
// 問題があれば *ValidationError を返す。
func validateAbsenceEventRequest(req model.AbsenceEventRequest) error {
	// Reason: 未指定（空文字）は許容する。指定時は4値のいずれかであること。
	if req.Reason != "" && !req.Reason.IsValid() {
		return &ValidationError{Message: "欠席理由の指定が不正です"}
	}

	// Detail: trim 後 200 文字以内。未入力は可。
	detail := strings.TrimSpace(req.Detail)
	if len([]rune(detail)) > absenceDetailMaxLen {
		return &ValidationError{Message: fmt.Sprintf("詳細は%d文字以内で入力してください", absenceDetailMaxLen)}
	}

	return nil
}

// buildAbsenceNotification は欠席連絡メールの件名・本文を組み立てる。
// 件名の title が長い場合は末尾側を切り詰めて、件名全体を
// notificationSubjectMaxLen (255) 文字以内に収める。本文には切り詰め前の title を使う。
func buildAbsenceNotification(title, username string, reason model.AbsenceReason, detail string) (subject, body string) {
	// 件名は「【欠席連絡】「{title}」への参加キャンセル」。
	// プレフィックス・サフィックスを除いた title の上限を求め、超過分を末尾から切り詰める。
	maxTitleLen := notificationSubjectMaxLen - len([]rune(absenceSubjectPrefix)) - len([]rune(absenceSubjectSuffix))
	subjectTitle := title
	if runes := []rune(subjectTitle); len(runes) > maxTitleLen {
		subjectTitle = string(runes[:maxTitleLen])
	}
	subject = absenceSubjectPrefix + subjectTitle + absenceSubjectSuffix

	var b strings.Builder
	b.WriteString("イベント名：")
	b.WriteString(title)
	b.WriteString("\n参加者名：")
	b.WriteString(username)
	b.WriteString("\n欠席理由：")
	b.WriteString(absenceReasonLabel(reason))
	if detail != "" {
		b.WriteString("\n詳細：")
		b.WriteString(detail)
	}
	body = b.String()

	return subject, body
}

// absenceReasonLabel は欠席理由のメール文面向けラベルを返す。
// 未指定（空文字）は「記載なし」を返す。
func absenceReasonLabel(reason model.AbsenceReason) string {
	switch reason {
	case "":
		return "記載なし"
	case model.AbsenceReasonIllness:
		return "体調不良"
	case model.AbsenceReasonFamily:
		return "家庭の都合"
	case model.AbsenceReasonWeatherTransport:
		return "天候・交通"
	case model.AbsenceReasonOther:
		return "その他"
	default:
		return ""
	}
}

// GetMyApplication はログイン中ユーザー自身の、指定イベントに対する申込内容を返す。
//
// repository が返す sentinel エラーをここで HTTP 向けエラーに変換する。
// 匿名申込は profile_id で識別できず、本メソッドの対象外（未申込と同じ扱い。ADR-0026）。
func (s *EventJoinService) GetMyApplication(
	ctx context.Context,
	eventID, profileID uuid.UUID,
) (model.MyEventApplicationResponse, error) {

	member, err := s.joinRepo.GetMemberByProfile(ctx, eventID, profileID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.MyEventApplicationResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrNotJoined):
			return model.MyEventApplicationResponse{}, &NotFoundError{Message: "このイベントに参加していません"}
		}
		return model.MyEventApplicationResponse{}, fmt.Errorf("get my application: %w", err)
	}

	return model.MyEventApplicationResponse{
		EventID:      eventID,
		Username:     member.Username,
		MailAddress:  member.MailAddress,
		PartySize:    member.PartySize,
		Participants: toParticipantResponses(member.Categories),
		CreatedAt:    member.CreatedAt,
	}, nil
}

// ListMembers はイベント主催者が参加者一覧を取得する。
//
// 認可・バリデーションは requireEventOwner ヘルパーに集約。
// エラーポリシー:
//   - イベントID不正 or イベント不存在 → *ValidationError（400）
//   - 主催者以外 or profileID 不正 → *ForbiddenError（403）
//
// 返却: created_at 昇順の参加者一覧。0件でも空配列で返す。
func (s *EventJoinService) ListMembers(
	ctx context.Context,
	profileID, eventID string,
) (model.EventMemberListResponse, error) {

	parsedEventID, err := requireEventOwner(ctx, s.eventRepo, profileID, eventID)
	if err != nil {
		return model.EventMemberListResponse{}, err
	}

	members, err := s.joinRepo.ListMembers(ctx, parsedEventID)
	if err != nil {
		return model.EventMemberListResponse{}, fmt.Errorf("list members: %w", err)
	}

	respMembers := make([]model.EventMemberResponse, 0, len(members))
	totalMembers := 0
	for _, m := range members {
		respMembers = append(respMembers, model.EventMemberResponse{
			Username:     m.Username,
			PartySize:    m.PartySize,
			Participants: toParticipantResponses(m.Categories),
			MailAddress:  m.MailAddress,
			CreatedAt:    m.CreatedAt,
			Profile:      m.Profile,
		})
		totalMembers += m.PartySize
	}

	return model.EventMemberListResponse{
		Members:      respMembers,
		TotalCount:   len(respMembers),
		TotalMembers: totalMembers,
	}, nil
}

// validateJoinEventRequest はリクエストの各フィールドを検証する。
// 問題があれば *ValidationError を返す。
func validateJoinEventRequest(req model.JoinEventRequest) error {
	// Username: trim 後に必須・255文字以内。
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return &ValidationError{Message: "ユーザー名は必須です"}
	}
	if len([]rune(username)) > 255 {
		return &ValidationError{Message: "ユーザー名は255文字以内で入力してください"}
	}

	// MailAddress: trim 後に必須・メール形式・255文字以内。
	mailAddress := strings.TrimSpace(req.MailAddress)
	if mailAddress == "" {
		return &ValidationError{Message: "メールアドレスは必須です"}
	}
	if len([]rune(mailAddress)) > 255 {
		return &ValidationError{Message: "メールアドレスは255文字以内で入力してください"}
	}
	if _, err := mail.ParseAddress(mailAddress); err != nil {
		return &ValidationError{Message: "メールアドレスの形式が不正です"}
	}

	// Participants: カテゴリ別の参加人数。1件以上必要で、合計が参加人数になる。
	if len(req.Participants) == 0 {
		return &ValidationError{Message: "参加人数の内訳は1件以上必要です"}
	}

	// 同一カテゴリの重複を防ぐ。表記ゆれ（大文字小文字）も同一とみなす。
	// カテゴリがイベントに実在するかは repository がトランザクション内で確認する。
	seen := make(map[string]struct{}, len(req.Participants))
	for _, p := range req.Participants {
		category := strings.TrimSpace(p.Category)
		if category == "" {
			return &ValidationError{Message: "参加者カテゴリは必須です"}
		}
		if len([]rune(category)) > 255 {
			return &ValidationError{Message: "参加者カテゴリは255文字以内で入力してください"}
		}
		if p.HeadCount < 1 {
			return &ValidationError{Message: "各カテゴリの人数は1人以上で入力してください"}
		}

		key := strings.ToLower(category)
		if _, dup := seen[key]; dup {
			return &ValidationError{
				Message: fmt.Sprintf("参加者カテゴリ「%s」が重複しています", category),
			}
		}
		seen[key] = struct{}{}
	}

	return nil
}
