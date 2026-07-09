package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// EventParticipationLogService はイベント参加状態ログ追加のビジネスロジックを担当する。
type EventParticipationLogService struct {
	logRepo repository.EventParticipationLogRepository
}

// NewEventParticipationLogService は Service を生成する。
func NewEventParticipationLogService(logRepo repository.EventParticipationLogRepository) *EventParticipationLogService {
	return &EventParticipationLogService{logRepo: logRepo}
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
