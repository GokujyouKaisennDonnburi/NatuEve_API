// Package middleware は Gin のミドルウェアを提供する。
//
// リクエストロギングと panic 回復(slog 連携)、認証(Supabase が発行する JWT を JWKS で検証)、
// CORS、リクエストボディのサイズ制限、クエリ文字列の検証、IP 単位のレート制限を提供する。
package middleware
