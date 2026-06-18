package server

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// swag が生成する OpenAPI ドキュメント。init() で登録するため blank import する。
	_ "github.com/GokujyouKaisennDonnburi/NatuIve_API/docs"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/config"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/handler"
	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/middleware"
)

// NewRouter は設定をもとに Gin のルーターを構築して返す。
func NewRouter(cfg config.Config) (*gin.Engine, error) {
	// gin.Default() の代わりに slog 連携のロガー/リカバリを使う。
	r := gin.New()
	r.Use(middleware.SlogLogger(), middleware.SlogRecovery())

	// 信頼するプロキシを設定（nil = どのプロキシも信頼しない）。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	// Swagger UI: http://<host>/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	registerRoutes(r)

	return r, nil
}

// registerRoutes は各ハンドラをルーターに登録する。
func registerRoutes(r *gin.Engine) {
	health := handler.NewHealthHandler()
	r.GET("/health", health.Check)
}
