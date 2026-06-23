// Package server は Gin ルーターの構築とルート定義を担う。
package server

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// swag が生成する OpenAPI ドキュメント。init() で登録するため blank import する。
	_ "github.com/GokujyouKaisennDonnburi/NatuIve_API/api"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/handler"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/repository"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/service"
)

// NewRouter は設定と DB 接続をもとに Gin のルーターを構築して返す。
//
// sqlDB が nil、または SUPABASE_JWKS_URL が未設定の場合、認証が必要な
// user 系ルートは登録しない(health などは常に有効)。
func NewRouter(cfg config.Config, sqlDB *sql.DB) (*gin.Engine, error) {
	// gin.Default() の代わりに slog 連携のロガー/リカバリを使う。
	r := gin.New()
	r.Use(middleware.SlogLogger(), middleware.SlogRecovery())

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

	// DB と JWKS が揃っているときのみ認証付きの user 系ルートを有効にする。
	if sqlDB == nil || cfg.SupabaseJWKSURL == "" {
		return nil
	}

	verifier, err := middleware.NewSupabaseVerifier(cfg)
	if err != nil {
		return err
	}

	profileRepo := repository.NewProfileRepository(sqlDB)
	profileSvc := service.NewProfileService(profileRepo)
	userHandler := handler.NewUserHandler(profileSvc)

	v1 := r.Group("/api/v1")
	v1.Use(verifier.RequireAuth())
	v1.GET("/me", userHandler.GetMe)

	return nil
}
