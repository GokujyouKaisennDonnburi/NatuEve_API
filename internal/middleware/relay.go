package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// RelayAuthHeader は中継用 Worker が秘密値を入れるヘッダー名（ADR-0034）。
// workers/api-relay/src/index.ts の RELAY_AUTH_HEADER と同じ値にする。
const RelayAuthHeader = "X-Relay-Auth"

// minRelaySecretLen は秘密値の最小の長さ（バイト数）。
const minRelaySecretLen = 32

// NewRelayGuard は、リクエストが中継用 Worker から届いたことを秘密ヘッダーで確かめる
// gin ミドルウェアを返す（ADR-0034）。
//
// secrets は受け付ける秘密値（値の差し替え中は新旧の 2 つ）。各値は minRelaySecretLen バイト以上でなければならない。
// clientIPHeader は Worker が利用者 IP を入れるヘッダー名で、呼び出し側が gin.Engine.TrustedPlatform に
// 同じ値を設定する前提。
// exemptPaths は確認から外すパス（URL のパスとの完全一致）。
//
// 秘密値は SHA-256 にしてから定数時間で比較し、受け付ける値をすべて比較し終えるまで打ち切らない。
// 一致すれば秘密ヘッダーを取り除いて次へ進む。一致しなければ秘密ヘッダーと clientIPHeader を取り除き
// （c.ClientIP() は接続元の IP を返すようになる）、exemptPaths なら次へ進み、それ以外は 403 で中断する。
// 秘密値と clientIPHeader の値はログに出さない。
func NewRelayGuard(secrets []string, clientIPHeader string, exemptPaths []string) (gin.HandlerFunc, error) {
	if len(secrets) == 0 {
		return nil, errors.New("no relay secret")
	}
	digests := make([][sha256.Size]byte, len(secrets))
	for i, s := range secrets {
		if len(s) < minRelaySecretLen {
			return nil, fmt.Errorf("relay secret must be at least %d bytes", minRelaySecretLen)
		}
		digests[i] = sha256.Sum256([]byte(s))
	}
	if clientIPHeader == "" {
		return nil, errors.New("no client IP header")
	}
	if strings.EqualFold(clientIPHeader, RelayAuthHeader) {
		return nil, fmt.Errorf("client IP header must differ from %s", RelayAuthHeader)
	}

	exempt := make(map[string]struct{}, len(exemptPaths))
	for _, p := range exemptPaths {
		exempt[p] = struct{}{}
	}

	return func(c *gin.Context) {
		verified := relaySecretMatches(c.GetHeader(RelayAuthHeader), digests)
		c.Request.Header.Del(RelayAuthHeader)
		if verified {
			c.Next()
			return
		}

		c.Request.Header.Del(clientIPHeader)
		if _, ok := exempt[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		slog.Warn("中継元の確認に失敗したリクエストを拒否しました",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
		)
		c.AbortWithStatusJSON(http.StatusForbidden, model.NewErrorResponse("forbidden", "このリクエストは受け付けられません"))
	}, nil
}

// relaySecretMatches は got の SHA-256 が digests のいずれかと一致すれば true を返す。
// 比較は定数時間で行い、一致しても残りの digests との比較を続ける。
func relaySecretMatches(got string, digests [][sha256.Size]byte) bool {
	sum := sha256.Sum256([]byte(got))
	matched := 0
	for i := range digests {
		matched |= subtle.ConstantTimeCompare(sum[:], digests[i][:])
	}
	return matched == 1
}
