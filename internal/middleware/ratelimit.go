package middleware

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// limiterTTL はこの期間アクセスのないクライアントのバケットを破棄する。
const limiterTTL = 10 * time.Minute

// sweepInterval はバケット掃除を行う最短間隔。
const sweepInterval = 3 * time.Minute

// ipLimiterEntry は1クライアント分のトークンバケットと最終アクセス時刻。
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter はクライアント IP ごとのトークンバケットでリクエストを制限する。
//
// c.ClientIP() をキーにするため、プロキシ配下では TRUSTED_PROXIES が正しく
// 設定されていることが前提（誤設定は X-Forwarded-For 偽装によるすり抜けを許す）。
// IPv6 はアドレスが実質無限に使えるため /64 プレフィックス単位でまとめて数える。
type IPRateLimiter struct {
	mu        sync.Mutex
	entries   map[string]*ipLimiterEntry
	lastSweep time.Time
	// limit は平均許容レート、burst は瞬間許容量。
	limit rate.Limit
	burst int
}

// NewIPRateLimiter は limit（平均レート）と burst（バースト許容量）で生成する。
//
// 例: NewIPRateLimiter(rate.Every(12*time.Second), 5) → 平均 5回/分・瞬間最大5回。
func NewIPRateLimiter(limit rate.Limit, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		entries:   make(map[string]*ipLimiterEntry),
		lastSweep: time.Now(),
		limit:     limit,
		burst:     burst,
	}
}

// Middleware は制限超過時に 429 を返す gin ミドルウェアを返す。
func (l *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(rateLimitKey(c.ClientIP()), time.Now()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, model.NewErrorResponse(
				"rate_limited",
				"リクエストが多すぎます。しばらくしてから再試行してください",
			))
			return
		}
		c.Next()
	}
}

// allow は key のバケットからトークンを1つ消費できるか判定する。
func (l *IPRateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	e, ok := l.entries[key]
	if !ok {
		e = &ipLimiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = now
	return e.limiter.Allow()
}

// sweep は前回の掃除から sweepInterval 以上経過していれば、
// limiterTTL を超えて未使用のバケットを破棄する（メモリの無限成長防止）。
// 呼び出し側が mu を保持していること。
func (l *IPRateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for key, e := range l.entries {
		if now.Sub(e.lastSeen) > limiterTTL {
			delete(l.entries, key)
		}
	}
}

// rateLimitKey はクライアント IP をレートリミットのキーへ正規化する。
//
// IPv4（v4-mapped 含む）はそのまま、IPv6 は /64 プレフィックスに丸める。
// パースできない値はそのままキーとして扱う（制限なしにはしない）。
func rateLimitKey(clientIP string) string {
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		return clientIP
	}
	if addr.Is4() || addr.Is4In6() {
		return addr.Unmap().String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}
