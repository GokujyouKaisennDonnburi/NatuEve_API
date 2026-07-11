// Package server は Gin ルーターの構築とルート定義を担う。
package server

import (
	"database/sql"
	"log/slog"
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
//
// 戻り値の *service.NotificationOutboxWorker は、イベントキャンセル通知の
// バックグラウンド送信ワーカー。sqlDB と Resend 設定（RESEND_API_KEY・MAIL_FROM）が
// 揃っている場合のみ生成され、それ以外は nil を返す（呼び出し元は nil チェックのうえ
// go worker.Run(ctx) すること。nil の場合は outbox に予約は溜まるが送信は行われない）。
func NewRouter(cfg config.Config, sqlDB *sql.DB) (*gin.Engine, *service.NotificationOutboxWorker, error) {

	// gin.Default() の代わりに slog 連携のロガー/リカバリを使う。
	r := gin.New()
	r.Use(middleware.SlogLogger(),
		middleware.SlogRecovery(),
		middleware.NewCORS(),
		middleware.BodyLimit(),
	)

	// 信頼するプロキシを設定（nil = どのプロキシも信頼しない）。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, nil, err
	}

	// Swagger UI: http://<host>/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	worker, err := registerRoutes(r, cfg, sqlDB)
	if err != nil {
		return nil, nil, err
	}

	return r, worker, nil
}

// join のレートリミット設定。参加申込は匿名で叩けるため、同一 IP からの
// 大量申込（架空メールでの定員埋め・DB 汚染）を平均 5回/分・バースト5回に絞る。
const (
	joinRateInterval = 12 * time.Second
	joinRateBurst    = 5
)

// registerRoutes は各ハンドラをルーターに登録する。
// 戻り値の *service.NotificationOutboxWorker については NewRouter の doc を参照。
func registerRoutes(r *gin.Engine, cfg config.Config, sqlDB *sql.DB) (*service.NotificationOutboxWorker, error) {
	health := handler.NewHealthHandler()
	r.GET("/health", health.Check)

	// DB が無ければ DB 依存のルートは何も登録しない。
	if sqlDB == nil {
		return nil, nil
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
	eventJoinRepo := repository.NewEventJoinRepository(sqlDB)
	eventJoinSvc := service.NewEventJoinService(eventJoinRepo, eventRepo)

	// Resend 設定（RESEND_API_KEY・MAIL_FROM）が揃っている場合のみ通知送信ワーカーを
	// 生成する。揃っていない場合、キャンセル API 自体は動く（outbox には予約される）が、
	// 送信するワーカーが存在しないため通知は届かない。運用者に気付けるよう警告を出す。
	var outboxWorker *service.NotificationOutboxWorker
	var mailer service.Mailer
	if cfg.ResendAPIKey != "" && cfg.MailFrom != "" {
		mailer = mail.NewResendClient(cfg.ResendAPIKey, cfg.MailFrom)
		outboxRepo := repository.NewEventNotificationOutboxRepository(sqlDB)
		outboxWorker = service.NewNotificationOutboxWorker(outboxRepo, eventJoinRepo, mailer)
	} else {
		slog.Warn("RESEND_API_KEY または MAIL_FROM が未設定のため通知送信ワーカーを起動しません。" +
			"イベントキャンセル時の通知は outbox に蓄積されますが送信されません")
	}

	// worker.Wake はメソッド自体が nil レシーバ安全なため、outboxWorker が nil
	// （Resend 未設定）でもそのまま注入してよい（呼んでも no-op になる）。
	eventCmdSvc := service.NewEventCommandService(eventRepo, store, outboxWorker.Wake)

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
		return outboxWorker, nil
	}

	verifier, err := middleware.NewSupabaseVerifier(cfg)
	if err != nil {
		return nil, err
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
	v1.POST("/tags", tagHandler.Create)

	v1.POST("/events/:id/leave", eventHandler.Leave)
	v1.GET("/events/:id/members", eventHandler.ListMembers)
	v1.POST("/events/:id/cancel", eventHandler.Cancel)
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
	// 失敗し毎回 500 になる）ため、両方揃っているときのみ登録する。mailer は
	// outboxWorker と同じ条件で上でも生成済みのため、ここでは使い回す。
	if mailer != nil {
		notifySvc := service.NewEventNotificationService(eventRepo, eventJoinRepo, mailer)
		notifyHandler := handler.NewEventNotificationHandler(notifySvc)
		v1.POST("/events/:id/notifications", notifyHandler.Send)
	}

	// cancel ルートは mailer 設定に関係なく登録する（outbox への予約は DB のみで
	// 完結するため。Resend 未設定でも予約は成功し、後で設定が揃ってから
	// ワーカーが遡って送信できる）。
	return outboxWorker, nil
}
