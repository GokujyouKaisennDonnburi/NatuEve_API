// Package server は Gin ルーターの構築とルート定義を担う。
package server

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// swag が生成する OpenAPI ドキュメント。init() で登録するため blank import する。
	_ "github.com/GokujyouKaisennDonnburi/NatuEve_API/api"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/handler"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/storage"
)

// NewRouter は設定と DB 接続をもとに Gin のルーターを構築して返す。
//
// sqlDB が nil、または SUPABASE_JWKS_URL が未設定の場合、認証が必要な
// user 系ルートは登録しない(health などは常に有効)。
func NewRouter(cfg config.Config, sqlDB *sql.DB) (*gin.Engine, error) {
	// gin.Default() の代わりに slog 連携のロガー/リカバリを使う。
	r := gin.New()
	r.Use(middleware.SlogLogger(), middleware.SlogRecovery(), middleware.NewCORS())

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
		store = storage.NewR2Client(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket)
	}

	// events 一覧は公開エンドポイント。DB があれば JWKS の有無に関わらず登録する。
	eventRepo := repository.NewEventRepository(sqlDB)
	eventQuerySvc := service.NewEventQueryService(eventRepo)
	eventCmdSvc := service.NewEventCommandService(eventRepo, store)

	profileRepo := repository.NewProfileRepository(sqlDB)
	profileSvc := service.NewProfileService(profileRepo)

	eventHandler := handler.NewEventHandler(eventQuerySvc, eventCmdSvc, profileSvc)

	v1Public := r.Group("/api/v1")
	v1Public.GET("/events", eventHandler.List)

	// events/{id} は公開エンドポイント。DB があれば JWKS の有無に関わらず登録する。
	v1Public.GET("/events/:id", eventHandler.GetByID)

	// user 系は認証が必要。DB と JWKS の両方が揃っているときのみ登録する。
	if cfg.SupabaseJWKSURL == "" {
		return nil
	}

	verifier, err := middleware.NewSupabaseVerifier(cfg)
	if err != nil {
		return err
	}

	userHandler := handler.NewUserHandler(profileSvc)

	v1 := r.Group("/api/v1")
	v1.Use(verifier.RequireAuth())
	v1.GET("/me", userHandler.GetMe)
	v1.POST("/events", eventHandler.Create)
	v1.GET("/profiles/:id", userHandler.GetProfile)

	// R2 設定がある場合のみ upload ルートを登録する（JWKS gating と同じ方針）。
	if store != nil {
		uploadSvc := service.NewUploadService(store)
		uploadHandler := handler.NewUploadHandler(uploadSvc)
		v1.POST("/uploads/presign", uploadHandler.PresignPut)
	}

	return nil
}
