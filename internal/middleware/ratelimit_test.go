package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// newRateLimitRouter はレートリミット付きのテスト用ルーターを組み立てるヘルパー。
func newRateLimitRouter(t *testing.T, limiter *IPRateLimiter) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/join", limiter.Middleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

// doRequest は指定した送信元 IP で POST し、ステータスコードを返すヘルパー。
func doRequest(t *testing.T, r *gin.Engine, remoteAddr string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/join", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestIPRateLimiterMiddleware(t *testing.T) {
	tests := []struct {
		name string
		// burst 回までは許可され、それ以降は 429 になることを requests で表現する。
		requests []struct {
			remoteAddr string
			wantStatus int
		}
	}{
		{
			name: "同一IPはバースト分を超えると429",
			requests: []struct {
				remoteAddr string
				wantStatus int
			}{
				{"203.0.113.1:1111", http.StatusOK},
				{"203.0.113.1:2222", http.StatusOK},
				{"203.0.113.1:3333", http.StatusTooManyRequests},
			},
		},
		{
			name: "別IPは独立してカウントされる",
			requests: []struct {
				remoteAddr string
				wantStatus int
			}{
				{"203.0.113.1:1111", http.StatusOK},
				{"203.0.113.1:1111", http.StatusOK},
				{"203.0.113.1:1111", http.StatusTooManyRequests},
				{"203.0.113.2:1111", http.StatusOK},
				{"203.0.113.2:1111", http.StatusOK},
			},
		},
		{
			name: "IPv6は同一/64プレフィックスでまとめてカウントされる",
			requests: []struct {
				remoteAddr string
				wantStatus int
			}{
				{"[2001:db8:1:2:aaaa::1]:1111", http.StatusOK},
				{"[2001:db8:1:2:bbbb::2]:2222", http.StatusOK},
				{"[2001:db8:1:2:cccc::3]:3333", http.StatusTooManyRequests},
				// 別の /64 は独立してカウントされる。
				{"[2001:db8:1:3::1]:4444", http.StatusOK},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 平均レートを極端に遅くし、テスト中のトークン補充を排除する（burst=2）。
			limiter := NewIPRateLimiter(rate.Every(time.Hour), 2)
			r := newRateLimitRouter(t, limiter)

			for i, req := range tt.requests {
				if got := doRequest(t, r, req.remoteAddr); got != req.wantStatus {
					t.Errorf("request[%d] (%s): status = %d, want %d", i, req.remoteAddr, got, req.wantStatus)
				}
			}
		})
	}
}

func TestIPRateLimiterSweep(t *testing.T) {
	limiter := NewIPRateLimiter(rate.Every(time.Hour), 1)
	now := time.Now()

	// 2つのクライアントのバケットを作る。
	limiter.allow("203.0.113.1", now)
	limiter.allow("203.0.113.2", now)
	if len(limiter.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(limiter.entries))
	}

	// TTL 経過後のアクセスで未使用バケットが掃除される。
	later := now.Add(limiterTTL + sweepInterval + time.Minute)
	limiter.allow("203.0.113.3", later)

	if len(limiter.entries) != 1 {
		t.Errorf("sweep 後 entries = %d, want 1（新規クライアントのみ）", len(limiter.entries))
	}
	if _, ok := limiter.entries["203.0.113.3"]; !ok {
		t.Error("新規クライアントのバケットが存在しない")
	}
}

func TestRateLimitKey(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{"IPv4はそのまま", "203.0.113.1", "203.0.113.1"},
		{"v4-mappedはIPv4に正規化", "::ffff:203.0.113.1", "203.0.113.1"},
		{"IPv6は/64に丸める", "2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::/64"},
		{"パース不能な値はそのままキーにする", "not-an-ip", "not-an-ip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rateLimitKey(tt.ip); got != tt.want {
				t.Errorf("rateLimitKey(%q) = %q, want %q", tt.ip, got, tt.want)
			}
		})
	}
}
