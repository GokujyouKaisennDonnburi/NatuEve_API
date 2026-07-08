package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
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

	// 参加登録（バリデーション済みの値を使う）
	member := &model.EventMember{
		EventID:     eventID,
		ProfileID:   profileID,
		Username:    strings.TrimSpace(req.Username),
		MailAddress: strings.TrimSpace(req.MailAddress),
		PartySize:   req.PartySize,
	}

	if err := s.joinRepo.Join(ctx, member); err != nil {
		switch {
		case errors.Is(err, repository.ErrEventNotFound):
			return model.JoinEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		case errors.Is(err, repository.ErrAlreadyJoined):
			return model.JoinEventResponse{}, &ConflictError{Code: "already_joined", Message: "既に参加しています"}
		case errors.Is(err, repository.ErrEventCapacityFull):
			return model.JoinEventResponse{}, &ConflictError{Code: "capacity_full", Message: "定員に達しています"}
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
		EventID:     member.EventID,
		ProfileID:   respProfileID,
		Username:    member.Username,
		MailAddress: member.MailAddress,
		PartySize:   member.PartySize,
		CreatedAt:   member.CreatedAt,
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
	for _, m := range members {
		var respProfileID *uuid.UUID
		if m.ProfileID.Valid {
			v := m.ProfileID.UUID
			respProfileID = &v
		}
		respMembers = append(respMembers, model.EventMemberResponse{
			Username:    m.Username,
			ProfileID:   respProfileID,
			PartySize:   m.PartySize,
			MailAddress: m.MailAddress,
			CreatedAt:   m.CreatedAt,
		})
	}

	return model.EventMemberListResponse{
		Members:    respMembers,
		TotalCount: len(respMembers),
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

	// PartySize: 代表者を含む参加人数。1以上。
	if req.PartySize < 1 {
		return &ValidationError{
			Message: "参加人数は1人以上で入力してください",
		}
	}

	return nil
}
