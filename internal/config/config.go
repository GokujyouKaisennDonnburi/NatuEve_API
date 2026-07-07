// Package config はアプリ全体の設定値を環境変数から読み込む。
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config はアプリ全体の設定値を保持する。環境変数から読み込む。
type Config struct {
	// Port はサーバの待ち受けポート。
	Port string
	// TrustedProxies は信頼するプロキシ(CIDR/IP)。nil = どのプロキシも信頼しない。
	TrustedProxies []string
	// DatabaseURL は Postgres の接続文字列。空なら DB に接続せず起動する。
	DatabaseURL string
	// AutoMigrate が true なら起動時にマイグレーションを自動適用する(開発用)。
	// 本番では false にし、デプロイ手順で `make migrate-up` を実行する。
	AutoMigrate bool
	// SupabaseJWKSURL は Supabase Auth の JWKS エンドポイント。
	// 空なら認証付きルート(user API)を登録しない。
	SupabaseJWKSURL string
	// SupabaseJWTAudience は検証時に要求する JWT の aud クレーム。
	// 未設定なら Supabase の既定値 "authenticated"。
	SupabaseJWTAudience string
	// R2AccountID は Cloudflare R2 のアカウント ID。
	// 空なら storage 系ルートを登録しない（JWKS gating と同じ方針）。
	R2AccountID string
	// R2AccessKeyID は R2 の S3 互換 API アクセスキー ID。
	R2AccessKeyID string
	// R2SecretAccessKey は R2 の S3 互換 API シークレットキー。
	R2SecretAccessKey string
	// R2Bucket は R2 バケット名。未設定なら "natuportal"。
	R2Bucket string
	// R2PublicBaseURL は公開バケット（R2 パブリックバケット/カスタムドメイン）の
	// 配信ベースURL。設定するとイベント等のレスポンスに imageUrls/pdfUrls を含める。
	// 末尾スラッシュは正規化されるため有無を問わない。未設定なら URL は付与しない。
	// 例: https://media.natuportal.com
	R2PublicBaseURL string
	// ResendAPIKey は Resend の API キー。
	// 空なら一斉送信系ルートを登録しない（R2 の gating と同じ方針）。
	ResendAPIKey string
	// MailFrom は送信元メールアドレス。
	MailFrom string
}

// Load は環境変数から Config を構築する。
//
// PORT が未設定なら 8085、TRUSTED_PROXIES が未設定なら nil(直接接続のみ信頼)。
func Load() Config {
	cfg := Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if cfg.Port == "" {
		cfg.Port = "8085"
	}
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = strings.Split(v, ",")
	}
	if v := os.Getenv("DB_AUTO_MIGRATE"); v != "" {
		// パースできない値は false 扱い(明示的に true/1 のときだけ有効)。
		cfg.AutoMigrate, _ = strconv.ParseBool(v)
	}
	cfg.SupabaseJWKSURL = os.Getenv("SUPABASE_JWKS_URL")
	cfg.SupabaseJWTAudience = os.Getenv("SUPABASE_JWT_AUD")
	if cfg.SupabaseJWTAudience == "" {
		cfg.SupabaseJWTAudience = "authenticated"
	}
	cfg.R2AccountID = os.Getenv("R2_ACCOUNT_ID")
	cfg.R2AccessKeyID = os.Getenv("R2_ACCESS_KEY_ID")
	cfg.R2SecretAccessKey = os.Getenv("R2_SECRET_ACCESS_KEY")
	cfg.R2Bucket = os.Getenv("R2_BUCKET")
	if cfg.R2Bucket == "" {
		cfg.R2Bucket = "natuportal"
	}
	cfg.R2PublicBaseURL = os.Getenv("R2_PUBLIC_BASE_URL")
	cfg.ResendAPIKey = os.Getenv("RESEND_API_KEY")
	cfg.MailFrom = os.Getenv("MAIL_FROM")
	return cfg
}
