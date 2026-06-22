// Package main は NatuIve API サーバのエントリポイント。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/db"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/server"
)

// @title			NatuIve API
// @version		1.0
// @description	NatuIve のバックエンド API
// @BasePath		/
func main() {
	// 構造化ログ(JSON)を既定ロガーに設定する。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("server exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run はサーバを起動し、終了シグナルを受けて graceful shutdown するまでを担う。
// os.Exit を呼ばずエラーを返すことで、defer によるクリーンアップを確実に実行する。
func run() error {
	// 開発用に .env を読み込む（無ければ環境変数をそのまま使う）。
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using environment variables")
	}

	cfg := config.Load()

	// DATABASE_URL があれば DB へ接続する(未設定なら DB なしで起動)。
	if cfg.DatabaseURL != "" {
		sqlDB, err := db.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		defer func() { _ = sqlDB.Close() }()
		slog.Info("database connected")

		// 開発用: AutoMigrate が有効なら起動時にマイグレーションを適用する。
		if cfg.AutoMigrate {
			if err := db.Migrate(context.Background(), sqlDB); err != nil {
				return fmt.Errorf("apply migrations: %w", err)
			}
			slog.Info("migrations applied")
		}
	}

	r, err := server.NewRouter(cfg)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
		// ヘッダ読み取りに時間制限を設ける（Slowloris 攻撃対策）。
		ReadHeaderTimeout: 10 * time.Second,
	}

	// SIGINT / SIGTERM を受け取るためのコンテキスト。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// サーバーを別 goroutine で起動し、起動失敗はチャネルで受け取る。
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// サーバーエラーか終了シグナルのいずれかを待つ。
	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}
	stop()
	slog.Info("shutting down...")

	// 進行中のリクエストを最大 10 秒待ってから終了する。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	slog.Info("server stopped")
	return nil
}
