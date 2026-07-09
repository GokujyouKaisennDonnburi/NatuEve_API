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

// EventParticipationLogService はイベント参加状態ログのビジネスロジックを担当する。
// event_participation_logs テーブルへの追記・最新状態取得を行う。
type EventParticipationLogService struct {
	logRepo   repository.EventParticipationLogRepository
	eventRepo repository.EventRepository
}

// NewEventParticipationLogService は Service を生成する。
func NewEventParticipationLogService(
	logRepo repository.EventParticipationLogRepository,
	eventRepo repository.EventRepository,
) *EventParticipationLogService {
	return &EventParticipationLogService{logRepo: logRepo, eventRepo: eventRepo}
}

// Create はイベント参加状態ログ(join/leave)を1件追記する。
//
// リクエストの validate タグは bind 時に強制されないため、
// action の値は本メソッドで手動検証する（service が信頼境界）。
func (s *EventParticipationLogService) Create(
	ctx context.Context,
	eventID uuid.UUID,
	profileID uuid.UUID,
	req model.CreateParticipationLogRequest,
) (model.ParticipationLogResponse, error) {

	// バリデーション: action は join または leave のみ許可する。
	if req.Action != "join" && req.Action != "leave" {
		return model.ParticipationLogResponse{}, &ValidationError{Message: "参加状態は join または leave を指定してください"}
	}

	log := &model.EventParticipationLog{
		EventID:   eventID,
		ProfileID: profileID,
		Action:    req.Action,
	}

	if err := s.logRepo.Create(ctx, log); err != nil {
		if errors.Is(err, repository.ErrEventNotFound) {
			return model.ParticipationLogResponse{}, &NotFoundError{Message: "イベントが見つかりません"}
		}
		return model.ParticipationLogResponse{}, fmt.Errorf("create participation log: %w", err)
	}

	return model.ParticipationLogResponse{
		ID:        log.ID,
		EventID:   log.EventID,
		ProfileID: log.ProfileID,
		Action:    log.Action,
		CreatedAt: log.CreatedAt,
	}, nil
}

// GetLatestStatus は指定イベントに対する認証ユーザー自身の最新参加状態を返す。
//
// 主催者権限は不要（本人の参加状態のみを返す）。
//
// エラーポリシー:
//   - eventID が空・UUID 不正 → *ValidationError（400）
//   - profileID が UUID 不正 → *ValidationError（400）
//   - イベント不存在 → *NotFoundError（404）
//   - 履歴1件もなし（sql.ErrNoRows） → 空レスポンス（action=null, participating=false, updatedAt=null）
//   - 上記以外の repo エラー → %w でラップして伝播（handler で 500）
func (s *EventParticipationLogService) GetLatestStatus(
	ctx context.Context,
	profileID, eventID string,
) (model.ParticipationStatusResponse, error) {

	parsedEventID, err := requireEventExists(ctx, s.eventRepo, eventID)
	if err != nil {
		return model.ParticipationStatusResponse{}, err
	}

	// profileID は認証済みユーザーから渡されるため本来 valid。
	// 不正な値が来た場合は処理を続けない（fail-closed）。
	parsedProfileID, err := uuid.Parse(strings.TrimSpace(profileID))
	if err != nil {
		return model.ParticipationStatusResponse{}, &ValidationError{Message: "ユーザーIDが不正です"}
	}

	log, err := s.logRepo.GetLatest(ctx, parsedEventID, parsedProfileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 履歴なし = 未参加。200 OK で空レスポンスを返す。
			return model.ParticipationStatusResponse{
				Action:        nil,
				Participating: false,
				UpdatedAt:     nil,
			}, nil
		}
		return model.ParticipationStatusResponse{}, fmt.Errorf("get latest participation status: %w", err)
	}

	action := log.Action
	updatedAt := log.CreatedAt

	return model.ParticipationStatusResponse{
		Action:        &action,
		Participating: action == "join",
		UpdatedAt:     &updatedAt,
	}, nil
}
