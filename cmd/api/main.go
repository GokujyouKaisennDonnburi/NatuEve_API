// Package main は NatuEve API サーバのエントリポイント。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/db"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/server"
)

// @title						NatuEve API
// @version					1.0
// @description				NatuEve のバックエンド API
// @BasePath					/
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Supabase Auth が発行した JWT を "Bearer <token>" 形式で指定する
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

	// SIGINT / SIGTERM を受け取るためのコンテキスト。通知送信ワーカーにも同じ ctx を渡し、
	// シャットダウンシグナルで自然に停止させる。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// DATABASE_URL があれば DB へ接続する(未設定なら DB なしで起動)。
	// ルーター構築まで接続を生かすため、スコープを run() 全体に広げる。
	var sqlDB *sql.DB
	if cfg.DatabaseURL != "" {
		conn, err := db.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		defer func() { _ = conn.Close() }()
		sqlDB = conn
		slog.Info("database connected")

		// 開発用: AutoMigrate が有効なら起動時にマイグレーションを適用する。
		if cfg.AutoMigrate {
			if err := db.Migrate(context.Background(), sqlDB); err != nil {
				return fmt.Errorf("apply migrations: %w", err)
			}
			slog.Info("migrations applied")
		}
	}

	r, notificationWorker, err := server.NewRouter(cfg, sqlDB)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	// イベントキャンセル通知の送信ワーカー（Resend 設定が揃っている場合のみ非 nil）を
	// 起動する。ctx のキャンセルで自然に停止する。
	// workerDone は Run の終了を待ち合わせるためのチャネル。defer conn.Close() より前に
	// ワーカーの終了（＝後始末の DB 更新の完了）を待つことで、Close 済みの接続に対する
	// クエリ実行を防ぐ。
	workerDone := make(chan struct{})
	if notificationWorker != nil {
		go func() {
			defer close(workerDone)
			notificationWorker.Run(ctx)
		}()
	} else {
		close(workerDone)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
		// ヘッダ読み取りに時間制限を設ける（Slowloris 攻撃対策）。
		ReadHeaderTimeout: 10 * time.Second,
	}

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

	// 通知ワーカーの終了を待つ。Run は ctx.Done で新規着手を止めて速やかに返る設計
	// のため、通常は即座に完了する。defer conn.Close() より前にここで待つことで、
	// ワーカーが後始末の DB 更新（MarkSent 等）を完了してから接続を閉じる。
	select {
	case <-workerDone:
	case <-time.After(10 * time.Second):
		slog.Warn("notification worker did not stop within timeout")
	}

	return nil
}
