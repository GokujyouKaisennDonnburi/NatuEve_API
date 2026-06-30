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
	Message string `json:"message" example:"タイトルは必須です"`
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
