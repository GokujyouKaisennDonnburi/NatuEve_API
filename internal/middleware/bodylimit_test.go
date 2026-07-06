package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newBodyLimitRouter はボディサイズ制限付きのテスト用ルーターを組み立てるヘルパー。
// ハンドラは ShouldBindJSON でボディを読み込み、MaxBytesError なら 413 を返す。
func newBodyLimitRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit())
	r.POST("/test", func(c *gin.Context) {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				c.Status(http.StatusRequestEntityTooLarge)
				return
			}
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

// doBodyLimitRequest はボディと ContentLength を指定して POST し、ステータスコードを返すヘルパー。
// contentLength に -1 を渡すと Content-Length ヘッダを設定しない（チャンク転送模倣）。
func doBodyLimitRequest(t *testing.T, r *gin.Engine, body []byte, contentLength int64) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = contentLength
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestBodyLimitMiddleware(t *testing.T) {
	smallBody := []byte(`{"data":"` + strings.Repeat("a", 100) + `"}`)
	// maxBodyBytes + 数バイト分の有効な JSON ボディ。
	largeBody := []byte(`{"data":"` + strings.Repeat("a", maxBodyBytes) + `"}`)

	tests := []struct {
		name          string
		body          []byte
		contentLength int64
		wantStatus    int
	}{
		{
			name:          "上限以下のボディは通過して200を返す",
			body:          smallBody,
			contentLength: int64(len(smallBody)),
			wantStatus:    http.StatusOK,
		},
		{
			name:          "Content-Length が上限超過なら即座に413を返す",
			body:          largeBody,
			contentLength: maxBodyBytes + 1,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name: "Content-Length 未設定でも実ボディが上限超過なら読み取り時に413を返す",
			body: largeBody,
			// -1 はチャンク転送（Content-Length 未知）を模倣する。
			// BodyLimit は ContentLength チェックをスキップするが MaxBytesReader が読み取り時に検出する。
			contentLength: -1,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newBodyLimitRouter(t)
			if got := doBodyLimitRequest(t, r, tt.body, tt.contentLength); got != tt.wantStatus {
				t.Errorf("status = %d, want %d", got, tt.wantStatus)
			}
		})
	}
}
