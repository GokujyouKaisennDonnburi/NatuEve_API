// Package server は Gin ルーターの構築とルート定義を担う。
package server

import (
	"database/sql"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"golang.org/x/time/rate"

	// swag が生成する OpenAPI ドキュメント。init() で登録するため blank import する。
	_ "github.com/GokujyouKaisennDonnburi/NatuEve_API/api"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/handler"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/mail"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/storage"
)

// NewRouter は設定と DB 接続をもとに Gin のルーターを構築して返す。
// sqlDB が nil、または SUPABASE_JWKS_URL が未設定の場合、認証が必要な
// user 系ルートは登録しない(health などは常に有効)。
func NewRouter(cfg config.Config, sqlDB *sql.DB) (*gin.Engine, error) {

	// gin.Default() の代わりに slog 連携のロガー/リカバリを使う。
	r := gin.New()
	r.Use(middleware.SlogLogger(),
		middleware.SlogRecovery(),
		middleware.NewCORS(),
		middleware.BodyLimit(),
	)

	// 信頼するプロキシを設定（nil = どのプロキシも信頼しない）。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	// Swagger UI: http://<host>/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	if err := registerRoutes(r, cfg, sqlDB); err != nil {
		return nil, err
	}

	return r, nil
}

// join のレートリミット設定。参加申込は匿名で叩けるため、同一 IP からの
// 大量申込（架空メールでの定員埋め・DB 汚染）を平均 5回/分・バースト5回に絞る。
const (
	joinRateInterval = 12 * time.Second
	joinRateBurst    = 5
)

// registerRoutes は各ハンドラをルーターに登録する。
func registerRoutes(r *gin.Engine, cfg config.Config, sqlDB *sql.DB) error {
	health := handler.NewHealthHandler()
	r.GET("/health", health.Check)

	// DB が無ければ DB 依存のルートは何も登録しない。
	if sqlDB == nil {
		return nil
	}

	// R2 設定があれば ObjectStore を生成する（nil 安全）。
	var store service.ObjectStore
	if cfg.R2AccountID != "" {
		store = storage.NewR2Client(
			cfg.R2AccountID,
			cfg.R2AccessKeyID,
			cfg.R2SecretAccessKey,
			cfg.R2Bucket,
		)
	}

	// events 一覧は公開エンドポイント。DB があれば JWKS の有無に関わらず登録する。
	eventRepo := repository.NewEventRepository(sqlDB)
	eventQuerySvc := service.NewEventQueryService(eventRepo, cfg.R2PublicBaseURL)
	eventCmdSvc := service.NewEventCommandService(eventRepo, store)
	eventJoinRepo := repository.NewEventJoinRepository(sqlDB)
	eventJoinSvc := service.NewEventJoinService(eventJoinRepo, eventRepo)

	eventParticipationLogRepo := repository.NewEventParticipationLogRepository(sqlDB)
	eventParticipationLogSvc := service.NewEventParticipationLogService(eventParticipationLogRepo, eventRepo)

	tagRepo := repository.NewTagRepository(sqlDB)
	tagSvc := service.NewTagService(tagRepo)

	profileRepo := repository.NewProfileRepository(sqlDB)
	profileSvc := service.NewProfileService(profileRepo)

	reportRepo := repository.NewReportRepository(sqlDB)
	reportCmdSvc := service.NewReportCommandService(reportRepo, eventRepo, store)
	reportQuerySvc := service.NewReportQueryService(reportRepo, cfg.R2PublicBaseURL)

	eventHandler := handler.NewEventHandler(
		eventQuerySvc,
		eventCmdSvc,
		profileSvc,
		eventJoinSvc,
	)
	tagHandler := handler.NewTagHandler(tagSvc)
	userHandler := handler.NewUserHandler(profileSvc)
	reportHandler := handler.NewReportHandler(reportCmdSvc, reportQuerySvc)
	eventParticipationLogHandler := handler.NewEventParticipationLogHandler(eventParticipationLogSvc)

	v1Public := r.Group("/api/v1")
	v1Public.GET("/events", eventHandler.List)
	v1Public.GET("/tags", tagHandler.List)

	// events/{id} は公開エンドポイント。DB があれば JWKS の有無に関わらず登録する。
	v1Public.GET("/events/:id", eventHandler.GetByID)

	v1Public.GET("/profiles/:id", userHandler.GetProfile)

	// events/{id}/report は公開エンドポイント（1イベント1レポート）。
	v1Public.GET("/events/:id/report", reportHandler.GetByEventID)

	// join は匿名で叩けるため IP レートリミットを掛ける。
	joinLimiter := middleware.NewIPRateLimiter(rate.Every(joinRateInterval), joinRateBurst).Middleware()

	// user 系は認証が必要。DB と JWKS の両方が揃っているときのみ登録する。
	// JWKS 未設定の場合: join ルートのみ認証なし（常に匿名参加）で登録して終了する。
	if cfg.SupabaseJWKSURL == "" {
		v1Public.POST("/events/:id/join", joinLimiter, eventHandler.Join)
		return nil
	}

	verifier, err := middleware.NewSupabaseVerifier(cfg)
	if err != nil {
		return err
	}

	// join は認証任意（OptionalAuth）: ログイン時のみ profileId を記録する。
	v1Optional := r.Group("/api/v1")
	v1Optional.Use(verifier.OptionalAuth())
	v1Optional.POST("/events/:id/join", joinLimiter, eventHandler.Join)

	v1 := r.Group("/api/v1")
	v1.Use(verifier.RequireAuth())

	v1.GET("/me", userHandler.GetMe)
	v1.PATCH("/me", userHandler.UpdateMe)
	v1.POST("/events", eventHandler.Create)

	v1.GET("/events/:id/members", eventHandler.ListMembers)
	v1.GET("/events/:id/participation-logs", eventParticipationLogHandler.GetLatestStatus)
	v1.POST("/events/:id/participation-logs", eventParticipationLogHandler.Create)

	v1.POST("/reports", reportHandler.Create)

	// R2 設定がある場合のみ upload ルートを登録する（JWKS gating と同じ方針）。
	if store != nil {
		uploadSvc := service.NewUploadService(store)
		uploadHandler := handler.NewUploadHandler(uploadSvc)
		v1.POST("/uploads/presign", uploadHandler.PresignPut)
	}

	// Resend 設定がある場合のみ通知ルートを登録する（R2 gating と同じ方針）。
	// API キーだけでは送信できない（送信元 MAIL_FROM が無いと全送信が Resend 側で
	// 失敗し毎回 500 になる）ため、両方揃っているときのみ登録する。
	if cfg.ResendAPIKey != "" && cfg.MailFrom != "" {
		mailer := mail.NewResendClient(cfg.ResendAPIKey, cfg.MailFrom)
		notifySvc := service.NewEventNotificationService(eventRepo, eventJoinRepo, mailer)
		notifyHandler := handler.NewEventNotificationHandler(notifySvc)
		v1.POST("/events/:id/notifications", notifyHandler.Send)
	}

	return nil
}
