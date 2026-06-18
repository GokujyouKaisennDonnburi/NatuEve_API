package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/server"
)

//	@title			NatuIve API
//	@version		1.0
//	@description	NatuIve のバックエンド API
//	@BasePath		/
func main() {
	// 構造化ログ(JSON)を既定ロガーに設定する。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// 開発用に .env を読み込む（無ければ環境変数をそのまま使う）。
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using environment variables")
	}

	cfg := config.Load()

	r, err := server.NewRouter(cfg)
	if err != nil {
		slog.Error("failed to build router", slog.Any("error", err))
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// SIGINT / SIGTERM を受け取るためのコンテキスト。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// サーバーを別 goroutine で起動する。
	go func() {
		slog.Info("server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 終了シグナルを待つ。
	<-ctx.Done()
	stop()
	slog.Info("shutting down...")

	// 進行中のリクエストを最大 10 秒待ってから終了する。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", slog.Any("error", err))
		os.Exit(1)
	}
	slog.Info("server stopped")
}
