// Package middleware は Gin のミドルウェアを提供する。
//
// リクエストロギングと panic 回復(slog 連携)を実装済み。
// 認証(Supabase が発行する JWT を JWKS で検証)・CORS などは今後ここに追加する。
package middleware
