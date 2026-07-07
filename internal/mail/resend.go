// Package mail は Resend を使ったメール送信を提供する。
package mail

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

// resendBatchMaxSize は Resend の一括送信 API 1リクエストあたりの最大件数。
const resendBatchMaxSize = 100

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

		if _, err := c.client.Batch.SendWithContext(ctx, requests); err != nil {
			return fmt.Errorf("resend batch send: %w", err)
		}
	}

	return nil
}
