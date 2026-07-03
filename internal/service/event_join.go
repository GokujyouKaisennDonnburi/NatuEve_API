package service

import (
	"context"
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
type ConflictError struct {
	Message string
}

// Error は error インターフェイスを実装する。
func (e *ConflictError) Error() string {
	return e.Message
}

// EventJoinService はイベント参加申込のビジネスロジックを担当する。
type EventJoinService struct {
	repo repository.EventJoinRepository
}

// NewEventJoinService は Service を生成する。
func NewEventJoinService(repo repository.EventJoinRepository) *EventJoinService {
	return &EventJoinService{repo: repo}
}

// Join はイベント参加処理を行う。
//
// profileID が Invalid（匿名参加）の場合は profile_id を NULL として登録する。
// バリデーション → イベント存在確認 → 重複参加確認 → 定員確認 → 参加登録の順に処理する。
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

	// バリデーション済みの値を使う
	username := strings.TrimSpace(req.Username)
	mailAddress := strings.TrimSpace(req.MailAddress)

	// イベント存在確認
	exists, err := s.repo.ExistsEvent(ctx, eventID)
	if err != nil {
		return model.JoinEventResponse{}, fmt.Errorf("exists event: %w", err)
	}
	if !exists {
		return model.JoinEventResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
	}

	// 重複参加確認（同一 mail_address またはログイン時は同一 profile_id）
	joined, err := s.repo.ExistsMember(ctx, eventID, profileID, mailAddress)
	if err != nil {
		return model.JoinEventResponse{}, fmt.Errorf("exists member: %w", err)
	}
	if joined {
		return model.JoinEventResponse{}, &ConflictError{Message: "既に参加しています"}
	}

	// 定員取得（0 = 定員なし）
	capacity, err := s.repo.GetCapacity(ctx, eventID)
	if err != nil {
		return model.JoinEventResponse{}, fmt.Errorf("get capacity: %w", err)
	}

	// 現在参加人数取得
	memberCount, err := s.repo.CountMembers(ctx, eventID)
	if err != nil {
		return model.JoinEventResponse{}, fmt.Errorf("count members: %w", err)
	}

	// 定員チェック（capacity == 0 は「定員なし」）
	if capacity != 0 && memberCount >= capacity {
		return model.JoinEventResponse{}, &ConflictError{Message: "定員に達しています"}
	}

	// 参加登録
	member := &model.EventMember{
		EventID:     eventID,
		ProfileID:   profileID,
		Username:    username,
		MailAddress: mailAddress,
	}

	if err := s.repo.Join(ctx, member); err != nil {
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
		CreatedAt:   member.CreatedAt,
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

	return nil
}
