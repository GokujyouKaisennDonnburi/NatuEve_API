package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// conflictCode は ConflictError の Code を返す。
// Code が空の場合は既定値 "conflict" を返す。
func conflictCode(ce *service.ConflictError) string {
	if ce.Code != "" {
		return ce.Code
	}
	return "conflict"
}

// Join はイベント参加申込 API。
//
//	@Summary		イベント参加
//	@Description	認証は任意。ログイン時のみ profileId が記録される。
//	@Description	Authorization ヘッダなし → 匿名参加（profileId = null）。
//	@Description	ヘッダありでトークンが無効 → 401 で中断。
//	@Description	ヘッダありで有効 → profileId を記録してログイン参加。
//	@Description	参加人数はカテゴリ別の内訳（participants）で送る。カテゴリにはイベントの費用カテゴリ名
//	@Description	（イベント詳細の costs[].category）を指定する。大文字小文字は区別しない。
//	@Description	合計人数（partySize）はサーバーが内訳から算出するため、リクエストでは送らない。
//	@Description	0人のカテゴリは送らない。存在しないカテゴリ・重複カテゴリ・0以下の人数は400を返す。
//	@Description	イベント定員を超える場合は409 Conflict（capacity_full）を返す。
//	@Description	申込期限ありイベントで期限経過後に呼ぶと 409 Conflict（deadline_passed）を返す
//	@Description	（期限なしイベントは常時申し込める）。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"イベントID"
//	@Param			body	body		model.JoinEventRequest	true	"参加申込"
//	@Success		201		{object}	model.JoinEventResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		404		{object}	model.NotFoundErrorResponse
//	@Failure		409		{object}	model.JoinConflictErrorResponse	"already_joined: 既に参加しています / capacity_full: 定員に達しています / deadline_passed: 申込期限経過後"
//	@Failure		413		{object}	model.RequestTooLargeErrorResponse
//	@Failure		429		{object}	model.RateLimitedErrorResponse
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/join [post]
func (h *EventHandler) Join(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証情報の取得（任意）。
	// OptionalAuth ミドルウェアにより:
	//   - ヘッダなし → AuthFromContext は (_, false) を返す → 匿名参加
	//   - ヘッダありで無効 → ミドルウェアが 401 で中断済みのためここには到達しない
	//   - ヘッダありで有効 → (authUser, true)
	var profileID uuid.NullUUID
	if authUser, ok := middleware.AuthFromContext(c); ok {
		parsed, parseErr := uuid.Parse(authUser.ID)
		if parseErr != nil {
			c.JSON(
				http.StatusUnauthorized,
				model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
			)
			return
		}
		profileID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	// JSON受け取り
	var req model.JoinEventRequest
	if !bindJSON(c, &req) {
		return
	}

	// Service呼び出し
	resp, err := h.joinSvc.Join(c.Request.Context(), eventID, profileID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse("invalid_request", ve.Message),
			)
			return
		}

		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		var ce *service.ConflictError
		if errors.As(err, &ce) {
			c.JSON(
				http.StatusConflict,
				model.NewErrorResponse(conflictCode(ce), ce.Message),
			)
			return
		}

		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加申込に失敗しました"),
		)
		return
	}

	// 成功
	c.JSON(http.StatusCreated, resp)
}

// Leave はイベント参加キャンセル API。
//
//	@Summary		イベント参加キャンセル
//	@Description	認証必須。ログイン参加者が自身の参加を取り消す。
//	@Description	参加行を削除し、参加状態ログへ action=leave を1件追記する。
//	@Description	匿名参加（profileId=null）は本 API の対象外。
//	@Description	申込期限ありイベントで期限経過後の呼び出しは 409 Conflict（deadline_passed）で拒否される。
//	@Description	期限経過後のキャンセルは欠席連絡 API（POST /api/v1/events/{id}/absence）を利用する。
//	@Description	そのイベントに参加していない場合は 404 not_found を返す。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.LeaveEventResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse	"not_found: イベント不存在 または 未参加"
//	@Failure		409	{object}	model.LeaveConflictErrorResponse	"deadline_passed: 申込期限経過後"
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/leave [post]
func (h *EventHandler) Leave(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証必須。RequireAuth ミドルウェア配下だが、防御的に確認する。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}
	profileID, err := uuid.Parse(authUser.ID)
	if err != nil {
		c.JSON(
			http.StatusUnauthorized,
			model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
		)
		return
	}

	resp, err := h.joinSvc.Leave(c.Request.Context(), eventID, profileID)
	if err != nil {
		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		var ce *service.ConflictError
		if errors.As(err, &ce) {
			c.JSON(
				http.StatusConflict,
				model.NewErrorResponse(conflictCode(ce), ce.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		slog.Error("イベント参加キャンセルに失敗しました",
			slog.String("event_id", eventID.String()),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加キャンセルに失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Absence はイベント欠席連絡 API（ADR-0031）。
//
//	@Summary		イベント欠席連絡
//	@Description	認証必須。ログイン参加者が欠席理由を添えて参加を取り消す。
//	@Description	参加行を削除し、参加状態ログへ action=absence を1件追記する。
//	@Description	主催者宛ての欠席連絡メールを outbox に予約し、非同期で送信する（レスポンスには含まれない）。
//	@Description	reason は任意。指定する場合は illness(体調不良) / family(家庭の都合) /
//	@Description	weather_transport(天候・交通) / other(その他) の4値のいずれか。
//	@Description	reason 未指定時はレスポンスの reason が null になり、主催者メールには「記載なし」と記す。
//	@Description	detail は任意（trim 後200文字以内）。未指定時はレスポンスの detail が null になる。
//	@Description	申込期限ありイベントで期限前に呼ぶと 409 Conflict（before_deadline）を返す
//	@Description	（期限前のキャンセルは参加キャンセル API を利用する）。
//	@Description	end_date 経過後は 409 Conflict（event_ended）、取消済みイベントは 409 Conflict（event_cancelled）を返す。
//	@Description	そのイベントに参加していない場合は 404 not_found を返す。
//	@Tags			event
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"イベントID"
//	@Param			body	body		model.AbsenceEventRequest	true	"欠席連絡"
//	@Success		200		{object}	model.AbsenceEventResponse
//	@Failure		400		{object}	model.ValidationErrorResponse
//	@Failure		401		{object}	model.UnauthorizedErrorResponse
//	@Failure		404		{object}	model.NotFoundErrorResponse	"not_found: イベント不存在 または 未参加"
//	@Failure		409		{object}	model.AbsenceConflictErrorResponse	"before_deadline: 申込期限前 / event_ended: 終了済み / event_cancelled: 取消済み"
//	@Failure		500		{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/absence [post]
func (h *EventHandler) Absence(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証必須。RequireAuth ミドルウェア配下だが、防御的に確認する。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}
	profileID, err := uuid.Parse(authUser.ID)
	if err != nil {
		c.JSON(
			http.StatusUnauthorized,
			model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
		)
		return
	}

	// JSON受け取り
	var req model.AbsenceEventRequest
	if !bindJSON(c, &req) {
		return
	}

	// Service呼び出し
	resp, err := h.joinSvc.Absence(c.Request.Context(), eventID, profileID, req)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse("invalid_request", ve.Message),
			)
			return
		}

		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		var ce *service.ConflictError
		if errors.As(err, &ce) {
			c.JSON(
				http.StatusConflict,
				model.NewErrorResponse(conflictCode(ce), ce.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		slog.Error("イベント欠席連絡に失敗しました",
			slog.String("event_id", eventID.String()),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "欠席連絡に失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMyApplication はログイン中ユーザー自身の申込内容取得 API。
//
//	@Summary		イベント申込内容取得
//	@Description	認証必須。ログイン中ユーザー自身の、指定イベントに対する申込内容を返す。
//	@Description	参加費（金額）は含まない。カテゴリごとの金額はイベント詳細（GET /api/v1/events/{id}）の costs を参照する。
//	@Description	participants はカテゴリ名の昇順。申込時にカテゴリ別の内訳が必須のため1件以上返る。
//	@Description	未申込・参加キャンセル済み・匿名で申し込んだ場合は 404 を返す。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.MyEventApplicationResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		404	{object}	model.NotFoundErrorResponse	"not_found: イベント不存在 または 未申込"
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/members/me [get]
func (h *EventHandler) GetMyApplication(c *gin.Context) {

	// パスパラメータからイベントID取得
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			model.NewErrorResponse("invalid_request", "イベントIDが不正です"),
		)
		return
	}

	// 認証必須。RequireAuth ミドルウェア配下だが、防御的に確認する。
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}
	profileID, err := uuid.Parse(authUser.ID)
	if err != nil {
		c.JSON(
			http.StatusUnauthorized,
			model.NewErrorResponse("unauthorized", "ユーザーIDが不正です"),
		)
		return
	}

	resp, err := h.joinSvc.GetMyApplication(c.Request.Context(), eventID, profileID)
	if err != nil {
		var nfe *service.NotFoundError
		if errors.As(err, &nfe) {
			c.JSON(
				http.StatusNotFound,
				model.NewErrorResponse("not_found", nfe.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		slog.Error("申込内容の取得に失敗しました",
			slog.String("event_id", eventID.String()),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "申込内容の取得に失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListMembers godoc
//
//	@Summary		イベント参加者一覧取得
//	@Description	イベント主催者が、参加者一覧を取得する。主催者のみ閲覧可能。
//	@Description	profile は参加者のプロフィールサマリー。匿名参加の場合は null。
//	@Description	participants は申込のカテゴリ別人数（カテゴリ名の昇順）。内訳を持たない申込では空配列。
//	@Description	イベント不存在は 400 invalid_request（兄弟エンドポイントと統一）。
//	@Tags			event
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"イベントID"
//	@Success		200	{object}	model.EventMemberListResponse
//	@Failure		400	{object}	model.ValidationErrorResponse
//	@Failure		401	{object}	model.UnauthorizedErrorResponse
//	@Failure		403	{object}	model.ForbiddenErrorResponse
//	@Failure		500	{object}	model.InternalErrorResponse
//	@Router			/api/v1/events/{id}/members [get]
func (h *EventHandler) ListMembers(c *gin.Context) {
	authUser, ok := middleware.AuthFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, model.NewErrorResponse("unauthorized", "認証が必要です"))
		return
	}

	eventID := c.Param("id")

	resp, err := h.joinSvc.ListMembers(c.Request.Context(), authUser.ID, eventID)
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			c.JSON(
				http.StatusBadRequest,
				model.NewErrorResponse("invalid_request", ve.Message),
			)
			return
		}

		var fe *service.ForbiddenError
		if errors.As(err, &fe) {
			c.JSON(
				http.StatusForbidden,
				model.NewErrorResponse("forbidden", fe.Message),
			)
			return
		}

		// 想定外エラー（DB エラー等）は真因をログに残す。
		// クライアントには詳細を返さないため、調査はこのログで行う。
		slog.Error("参加者一覧取得に失敗しました",
			slog.String("event_id", eventID),
			slog.Any("error", err),
		)
		c.JSON(
			http.StatusInternalServerError,
			model.NewErrorResponse("internal_error", "参加者一覧の取得に失敗しました"),
		)
		return
	}

	c.JSON(http.StatusOK, resp)
}
