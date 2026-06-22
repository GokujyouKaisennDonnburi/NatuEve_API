// Package db はデータベース接続の確立とマイグレーションの適用を担う。
//
// マイグレーションファイル(db/migrations/*.sql)を embed しており、
// 起動時の自動適用(開発用)と CLI(goose)からの適用の双方で同じ定義を使う。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	// database/sql 用の pgx ドライバ("pgx")を登録する。
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationsFS はマイグレーション SQL を実行ファイルに埋め込む。
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir は embed 内のマイグレーション格納ディレクトリ名。
const migrationsDir = "migrations"

// Open は DSN を使って Postgres への *sql.DB を開き、疎通確認する。
//
// 呼び出し側は使い終わったら Close すること。
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return sqlDB, nil
}

// Migrate は埋め込んだマイグレーションを最新まで適用する(goose up 相当)。
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
