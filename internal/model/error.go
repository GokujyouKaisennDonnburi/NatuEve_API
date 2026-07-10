package model

// ErrorResponse は API のエラーレスポンスの統一フォーマット。
//
// 形式: {"error": {"code": "...", "message": "..."}}
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody は ErrorResponse のエラー本体。
//
// example は全エンドポイント共有の図示用。実際の code/message はエラーごとに
// 異なる（認証エラーなら unauthorized 等）ため、特定の状況を連想させない中立な値にしている。
type ErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"internal_error"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"サーバー内部でエラーが発生しました"`
}

// NewErrorResponse は code と message から ErrorResponse を組み立てる。
func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{Code: code, Message: message}}
}

// --- ドキュメント専用エラーレスポンス型 ---
// 実体は ErrorResponse と同じ {"error": {"code","message"}} 形式。
// swag がステータスコード別に異なる example を出力できるよう、型として分離している。
// ランタイムでは使用せず、swaggerコメントの @Failure 参照専用。

// ValidationErrorResponse は入力検証エラー(HTTP 400)のドキュメント用レスポンス型。
type ValidationErrorResponse struct {
	Error ValidationErrorBody `json:"error"`
}

// ValidationErrorBody は ValidationErrorResponse のエラー本体。
type ValidationErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"invalid_request"`
	// Message は人間向けのエラーメッセージ。
	// JSON バインド失敗時は「リクエストボディが不正です」（全エンドポイント共通）、
	// フィールド検証エラー時は「タイトルは必須です」のように原因ごとの文言が入る。
	Message string `json:"message" example:"リクエストボディが不正です"`
}

// UnauthorizedErrorResponse は認証エラー(HTTP 401)のドキュメント用レスポンス型。
type UnauthorizedErrorResponse struct {
	Error UnauthorizedErrorBody `json:"error"`
}

// UnauthorizedErrorBody は UnauthorizedErrorResponse のエラー本体。
type UnauthorizedErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"unauthorized"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"認証が必要です"`
}

// ForbiddenErrorResponse は認可エラー(HTTP 403)のドキュメント用レスポンス型。
type ForbiddenErrorResponse struct {
	Error ForbiddenErrorBody `json:"error"`
}

// ForbiddenErrorBody は ForbiddenErrorResponse のエラー本体。
type ForbiddenErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"forbidden"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"このイベントにレポートを投稿する権限がありません"`
}

// NotFoundErrorResponse はリソース未検出エラー(HTTP 404)のドキュメント用レスポンス型。
type NotFoundErrorResponse struct {
	Error NotFoundErrorBody `json:"error"`
}

// NotFoundErrorBody は NotFoundErrorResponse のエラー本体。
type NotFoundErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"not_found"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"リソースが見つかりません"`
}

// ConflictErrorResponse はリソース競合エラー(HTTP 409)のドキュメント用レスポンス型。
type ConflictErrorResponse struct {
	Error ConflictErrorBody `json:"error"`
}

// ConflictErrorBody は ConflictErrorResponse のエラー本体。
type ConflictErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"conflict"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"既に参加しています"`
}

// JoinConflictErrorResponse はイベント参加申込の競合エラー(HTTP 409)のドキュメント用レスポンス型。
//
// 参加申込は競合の原因が2種類あるため、汎用の ConflictErrorResponse ではなく
// code を enum で明示する専用型を使う。
type JoinConflictErrorResponse struct {
	Error JoinConflictErrorBody `json:"error"`
}

// JoinConflictErrorBody は JoinConflictErrorResponse のエラー本体。
type JoinConflictErrorBody struct {
	// Code は競合の原因を表す機械可読なエラーコード。
	// already_joined = 既に参加済み / capacity_full = 定員到達。
	Code string `json:"code" example:"already_joined" enums:"already_joined,capacity_full"`
	// Message は人間向けのエラーメッセージ。
	// already_joined なら「既に参加しています」、capacity_full なら「定員に達しています」。
	Message string `json:"message" example:"既に参加しています"`
}

// GoneErrorResponse は廃止済み・無効化エンドポイントのエラー(HTTP 410)のドキュメント用レスポンス型。
type GoneErrorResponse struct {
	Error GoneErrorBody `json:"error"`
}

// GoneErrorBody は GoneErrorResponse のエラー本体。
type GoneErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"gone"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"このエンドポイントは現在無効です。イベント参加/キャンセルは別APIを利用してください"`
}

// RequestTooLargeErrorResponse はリクエストボディ超過エラー(HTTP 413)のドキュメント用レスポンス型。
type RequestTooLargeErrorResponse struct {
	Error RequestTooLargeErrorBody `json:"error"`
}

// RequestTooLargeErrorBody は RequestTooLargeErrorResponse のエラー本体。
type RequestTooLargeErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"request_too_large"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"リクエストボディが大きすぎます（上限1MB）"`
}

// RateLimitedErrorResponse はレート制限エラー(HTTP 429)のドキュメント用レスポンス型。
type RateLimitedErrorResponse struct {
	Error RateLimitedErrorBody `json:"error"`
}

// RateLimitedErrorBody は RateLimitedErrorResponse のエラー本体。
type RateLimitedErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"rate_limited"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"リクエストが多すぎます。しばらくしてから再試行してください"`
}

// InternalErrorResponse はサーバー内部エラー(HTTP 500)のドキュメント用レスポンス型。
type InternalErrorResponse struct {
	Error InternalErrorBody `json:"error"`
}

// InternalErrorBody は InternalErrorResponse のエラー本体。
type InternalErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"internal_error"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"サーバー内部でエラーが発生しました"`
}
