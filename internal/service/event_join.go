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
}

// NewEventJoinService は Service を生成する。
func NewEventJoinService(joinRepo repository.EventJoinRepository, eventRepo repository.EventRepository) *EventJoinService {
	return &EventJoinService{joinRepo: joinRepo, eventRepo: eventRepo}
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
