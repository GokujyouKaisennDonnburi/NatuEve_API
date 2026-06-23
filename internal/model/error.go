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
