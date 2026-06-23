package model

// ErrorResponse は API のエラーレスポンスの統一フォーマット。
//
// 形式: {"error": {"code": "...", "message": "..."}}
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody は ErrorResponse のエラー本体。
type ErrorBody struct {
	// Code は機械可読なエラーコード。
	Code string `json:"code" example:"unauthorized"`
	// Message は人間向けのエラーメッセージ。
	Message string `json:"message" example:"認証が必要です"`
}

// NewErrorResponse は code と message から ErrorResponse を組み立てる。
func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{Code: code, Message: message}}
}
