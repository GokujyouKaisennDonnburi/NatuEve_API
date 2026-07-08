package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// requireEventOwner は eventID のイベント投稿者が profileID のユーザーであることを
// 確認し、成功時のみパース済みイベント UUID を返す。
//
// 認可フローを 1 箇所に集約することで、3 つのエンドポイント
// (POST /events/{id}/notifications, POST /reports, GET /events/{id}/members)
// での fail-closed 挙動とエラーメッセージのドリフトを防ぐ。
//
// 戻り値のポリシー:
//   - eventID が空・UUID として不正 → *ValidationError
//   - イベントが存在しない → *ValidationError（400 invalid_request として返す）
//   - profileID が UUID として不正、または profileID ≠ events.profile_id → *ForbiddenError
//   - 上記以外の repo エラー → %w でラップしてそのまま伝播
//
// イベント不存在を 404 NotFound ではなく 400 ValidationError として返すのは、
// 同じ主催者チェックを持つ兄弟エンドポイントが 400 を採用しているため。
// クライアント側で「イベント不存在」を共通処理する想定に合わせている。
func requireEventOwner(
	ctx context.Context,
	eventRepo repository.EventRepository,
	profileID, eventID string,
) (uuid.UUID, error) {
	trimmedEventID := strings.TrimSpace(eventID)
	parsedEventID, err := uuid.Parse(trimmedEventID)
	if err != nil {
		return uuid.Nil, &ValidationError{Message: "イベントIDが不正です"}
	}

	// profileID は認証済みユーザーから渡されるため本来 valid。
	// 不正な値が来た場合は認可を通さない（fail-closed）。
	parsedProfileID, profileParseErr := uuid.Parse(profileID)
	if profileParseErr != nil {
		return uuid.Nil, &ForbiddenError{Message: "このイベントを操作する権限がありません"}
	}

	ownerID, err := eventRepo.GetOwnerProfileID(ctx, trimmedEventID)
	if errors.Is(err, repository.ErrEventNotFound) {
		return uuid.Nil, &ValidationError{Message: "指定されたイベントが存在しません"}
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get event owner: %w", err)
	}

	ownerUID, ownerErr := uuid.Parse(ownerID)
	if ownerErr != nil || ownerUID != parsedProfileID {
		return uuid.Nil, &ForbiddenError{Message: "このイベントを操作する権限がありません"}
	}

	return parsedEventID, nil
}
