package main

import (
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 開発用に .env を読み込む（無ければ環境変数をそのまま使う）
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables")
	}

	r := gin.Default()

	// 信頼するプロキシを環境変数で設定（未設定なら nil = どのプロキシも信頼しない）
	// 開発: 未設定でOK（直接接続）
	// 本番: Cloudflare/AWS などのプロキシ CIDR を TRUSTED_PROXIES に設定
	var trusted []string
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		trusted = strings.Split(v, ",")
	}
	if err := r.SetTrustedProxies(trusted); err != nil {
		log.Fatal(err)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
