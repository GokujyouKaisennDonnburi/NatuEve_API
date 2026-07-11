package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// outboxPollInterval はワーカーが event_notification_outbox をポーリングする周期。
const outboxPollInterval = 30 * time.Second

// outboxBatchSize は1周期あたりに処理する outbox 行の最大件数。
const outboxBatchSize = 50

// outboxMaxAttempts は1行あたりの最大試行回数。これに達すると status='failed' にする。
const outboxMaxAttempts = 8

// outboxBaseBackoff / outboxMaxBackoff はリトライの指数バックオフの基準値・上限値。
// 次回試行までの待機時間は min(outboxBaseBackoff << attempts, outboxMaxBackoff)。
const (
	outboxBaseBackoff = 30 * time.Second
	outboxMaxBackoff  = time.Hour
)

// NotificationOutboxWorker は event_notification_outbox テーブルをポーリングし、
// 未送信の通知メールを送信するバックグラウンドワーカー。
//
// 単一プロセスでの動作を前提とする。複数インスタンス化してワーカーを並列実行する場合、
// EventNotificationOutboxRepository.ListDue が返した行を複数ワーカーが同時に処理して
// しまう恐れがあるため、SKIP LOCKED 等による claim 処理を別途追加する必要がある
// （現状は未対応）。
type NotificationOutboxWorker struct {
	outboxRepo repository.EventNotificationOutboxRepository
	joinRepo   repository.EventJoinRepository
	mailer     Mailer

	// wakeCh はバッファ1の起床通知チャネル。イベントキャンセル直後など、
	// 次のポーリング周期を待たずに即座に処理させたい場合に使う。
	wakeCh chan struct{}
}

// NewNotificationOutboxWorker は NotificationOutboxWorker を生成する。
func NewNotificationOutboxWorker(
	outboxRepo repository.EventNotificationOutboxRepository,
	joinRepo repository.EventJoinRepository,
	mailer Mailer,
) *NotificationOutboxWorker {
	return &NotificationOutboxWorker{
		outboxRepo: outboxRepo,
		joinRepo:   joinRepo,
		mailer:     mailer,
		wakeCh:     make(chan struct{}, 1),
	}
}

// Wake はワーカーを即座に起床させる。バッファ1の非ブロッキング送信のため、
// 既に起床要求が溜まっている場合は何もしない（安全に無視できる）。
//
// nil レシーバでも panic しない（w が nil の *NotificationOutboxWorker でも安全に
// 呼び出せる）。これにより、呼び出し元（EventCommandService）はワーカーが
// 生成されているかどうかを気にせず worker.Wake をそのまま注入できる。
func (w *NotificationOutboxWorker) Wake() {
	if w == nil {
		return
	}
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// Run はワーカーのメインループを起動する。ctx がキャンセルされるまで動き続ける
// （graceful shutdown はシグナルの ctx を渡すことで自然に停止する）。
func (w *NotificationOutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	// 起動直後にも1回処理し、直前にキャンセルされた通知の送信を待たせない。
	w.processDue(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processDue(ctx)
		case <-w.wakeCh:
			w.processDue(ctx)
		}
	}
}

// processDue は送信対象の outbox 行を取得し、1件ずつ処理する。
// 個々の行の失敗は他の行の処理を止めない（slog に記録して次の行へ進む）。
func (w *NotificationOutboxWorker) processDue(ctx context.Context) {
	due, err := w.outboxRepo.ListDue(ctx, time.Now(), outboxBatchSize)
	if err != nil {
		slog.Error("outbox の取得に失敗しました", slog.Any("error", err))
		return
	}

	for _, item := range due {
		w.processOne(ctx, item)
	}
}

// processOne は outbox 1行分の送信を試みる。
//
// 参加者が0件の場合は送信不要のため sent 扱いにする。送信に成功すれば sent、
// 失敗すれば試行回数に応じて retry（次回試行日時を予約）または failed（最大試行回数
// 到達）を記録する。
func (w *NotificationOutboxWorker) processOne(ctx context.Context, item model.EventNotificationOutbox) {
	recipients, err := w.joinRepo.ListRecipients(ctx, item.EventID)
	if err != nil {
		slog.Error("outbox の宛先取得に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	if len(recipients) == 0 {
		if err := w.outboxRepo.MarkSent(ctx, item.ID); err != nil {
			slog.Error("outbox の sent 更新に失敗しました",
				slog.String("outbox_id", item.ID.String()),
				slog.Any("error", err),
			)
		}
		return
	}

	emails := make([]Email, 0, len(recipients))
	for _, r := range recipients {
		emails = append(emails, Email{
			To:      r.MailAddress,
			Subject: item.Subject,
			Text:    item.Body,
		})
	}

	if err := w.mailer.SendBatch(ctx, emails); err != nil {
		w.handleSendFailure(ctx, item, err)
		return
	}

	if err := w.outboxRepo.MarkSent(ctx, item.ID); err != nil {
		slog.Error("outbox の sent 更新に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
	}
}

// handleSendFailure は送信失敗時の後処理を行う。
// 更新後の試行回数が outboxMaxAttempts 以上なら failed、未満なら
// 指数バックオフで次回試行日時を予約した retry を記録する。
func (w *NotificationOutboxWorker) handleSendFailure(ctx context.Context, item model.EventNotificationOutbox, sendErr error) {
	nextAttempts := item.Attempts + 1

	if nextAttempts >= outboxMaxAttempts {
		if err := w.outboxRepo.MarkFailed(ctx, item.ID, sendErr.Error()); err != nil {
			slog.Error("outbox の failed 更新に失敗しました",
				slog.String("outbox_id", item.ID.String()),
				slog.Any("error", err),
			)
		}
		slog.Error("通知の送信に最終的に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Int("attempts", nextAttempts),
			slog.Any("error", sendErr),
		)
		return
	}

	backoff := backoffFor(nextAttempts)
	nextAttemptAt := time.Now().Add(backoff)
	if err := w.outboxRepo.MarkRetry(ctx, item.ID, nextAttemptAt, sendErr.Error()); err != nil {
		slog.Error("outbox の retry 更新に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
	}
	slog.Warn("通知の送信に失敗しました。リトライを予約します",
		slog.String("outbox_id", item.ID.String()),
		slog.Int("attempts", nextAttempts),
		slog.Time("next_attempt_at", nextAttemptAt),
		slog.Any("error", sendErr),
	)
}

// backoffFor は attempts 回目の失敗後の待機時間を返す。
// min(outboxBaseBackoff << attempts, outboxMaxBackoff)。
func backoffFor(attempts int) time.Duration {
	// attempts は outboxMaxAttempts(8) 未満のため、シフト量は十分小さく安全。
	backoff := outboxBaseBackoff << uint(attempts) //nolint:gosec // attempts は上限8未満で保証済み
	if backoff <= 0 || backoff > outboxMaxBackoff {
		return outboxMaxBackoff
	}
	return backoff
}
