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
}

// Load は環境変数から Config を構築する。
//
// PORT が未設定なら 8080、TRUSTED_PROXIES が未設定なら nil(直接接続のみ信頼)。
func Load() Config {
	cfg := Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = strings.Split(v, ",")
	}
	if v := os.Getenv("DB_AUTO_MIGRATE"); v != "" {
		// パースできない値は false 扱い(明示的に true/1 のときだけ有効)。
		cfg.AutoMigrate, _ = strconv.ParseBool(v)
	}
	return cfg
}
