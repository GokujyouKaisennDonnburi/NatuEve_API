// Package mail は Resend を使ったメール送信を提供する。
package mail

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/resend/resend-go/v2"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// resendBatchMaxSize は Resend の一括送信 API 1リクエストあたりの最大件数。
const resendBatchMaxSize = 100

// レート制限（Resend 既定は 2 リクエスト/秒）に当たった際のリトライ設定。
const (
	// maxSendAttempts は1チャンクあたりの最大試行回数（初回 + リトライ）。
	maxSendAttempts = 4
	// baseRetryBackoff は指数バックオフの基準待機時間。
	baseRetryBackoff = 500 * time.Millisecond
	// maxRetryBackoff は1回あたりの待機時間の上限。
	maxRetryBackoff = 8 * time.Second
)

// ResendClient は Resend API を使った service.Mailer 実装。
type ResendClient struct {
	client *resend.Client
	from   string
}

// NewResendClient は ResendClient を生成する。
//
// apiKey: Resend の API キー
// from: 送信元メールアドレス
func NewResendClient(apiKey, from string) *ResendClient {
	return &ResendClient{
		client: resend.NewClient(apiKey),
		from:   from,
	}
}

// SendBatch は emails を送信する。
//
// ADR-0004 に従い、各 Email は受信者ごとに個別のメールとして送信する
// （1通あたりの To は単一宛先。BCC は使わない）。
// Resend の一括送信 API は 1リクエストあたり最大 100件のため、
// resendBatchMaxSize 件ごとにチャンク分割してリクエストする。
func (c *ResendClient) SendBatch(ctx context.Context, emails []service.Email) error {
	for start := 0; start < len(emails); start += resendBatchMaxSize {
		end := start + resendBatchMaxSize
		if end > len(emails) {
			end = len(emails)
		}

		chunk := emails[start:end]
		requests := make([]*resend.SendEmailRequest, 0, len(chunk))
		for _, e := range chunk {
			requests = append(requests, &resend.SendEmailRequest{
				From:    c.from,
				To:      []string{e.To},
				Subject: e.Subject,
				Text:    e.Text,
			})
		}

		if err := c.sendChunkWithRetry(ctx, requests); err != nil {
			return err
		}
	}

	return nil
}

// sendChunkWithRetry は1チャンクを送信し、Resend のレート制限（429）に
// 当たった場合のみ指数バックオフでリトライする。
//
// レート制限による 429 はリクエストが処理される前に拒否されるため、
// チャンク全体を再送しても重複送信にはならない。レート制限以外のエラーは
// リトライせず即座に返す。リトライしても解消しなかった場合は
// service.ErrMailRateLimited をラップして返す（handler 層が 429 を返すため）。
func (c *ResendClient) sendChunkWithRetry(ctx context.Context, requests []*resend.SendEmailRequest) error {
	var lastErr error
	for attempt := 0; attempt < maxSendAttempts; attempt++ {
		_, err := c.client.Batch.SendWithContext(ctx, requests)
		if err == nil {
			return nil
		}
		// レート制限以外のエラー（ドメイン未検証・認証エラー等）はリトライしない。
		if !errors.Is(err, resend.ErrRateLimit) {
			return fmt.Errorf("resend batch send: %w", err)
		}

		lastErr = err
		// 最終試行後は待機せずループを抜ける。
		if attempt == maxSendAttempts-1 {
			break
		}

		wait := retryWait(err, attempt)
		select {
		case <-ctx.Done():
			return fmt.Errorf("resend batch send canceled while rate limited: %w", ctx.Err())
		case <-time.After(wait):
		}
	}

	return fmt.Errorf("resend batch send rate limited after %d attempts: %w: %w",
		maxSendAttempts, service.ErrMailRateLimited, lastErr)
}

// retryWait は次のリトライまでの待機時間を決める。
//
// Resend が Retry-After / Reset ヘッダ（秒）を返していればそれを優先し、
// 無ければ base * 2^attempt の指数バックオフにフォールバックする。
// いずれも maxRetryBackoff で上限を掛ける。
func retryWait(err error, attempt int) time.Duration {
	var rl *resend.RateLimitError
	if errors.As(err, &rl) {
		if d := parseSecondsHeader(rl.RetryAfter); d > 0 {
			return capWait(d)
		}
		if d := parseSecondsHeader(rl.Reset); d > 0 {
			return capWait(d)
		}
	}
	return capWait(baseRetryBackoff << attempt)
}

// parseSecondsHeader は秒数を表すヘッダ値をパースする。空・不正・非正なら 0 を返す。
func parseSecondsHeader(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// capWait は待機時間を maxRetryBackoff で頭打ちにする。
func capWait(d time.Duration) time.Duration {
	if d > maxRetryBackoff {
		return maxRetryBackoff
	}
	return d
}
