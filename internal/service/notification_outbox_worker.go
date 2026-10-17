package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// outboxPollInterval はワーカーが event_notification_outbox をポーリングする周期。
const outboxPollInterval = 30 * time.Second

// outboxBatchSize は1周期あたりに処理する outbox 行の最大件数。
const outboxBatchSize = 50

// outboxProcessBudget は ProcessOnce 1 回で新しい行を確保し続ける時間の上限。
// Cloud Scheduler の呼び出し間隔（1 分）より短くする（ADR-0034）。
const outboxProcessBudget = 50 * time.Second

// outboxMarkReserve は確保の期限（OutboxClaim.Deadline）のうち、送信結果の記録
// （MarkSent/MarkRetry/MarkFailed）のために残しておく時間。宛先取得と送信は、期限のこの時間前までに打ち切る。
const outboxMarkReserve = 10 * time.Second

// outboxMaxAttempts は1行あたりの最大試行回数。これに達すると status='failed' にする。
const outboxMaxAttempts = 8

// outboxBaseBackoff / outboxMaxBackoff はリトライの指数バックオフの基準値・上限値。
// 次回試行までの待機時間は min(outboxBaseBackoff << attempts, outboxMaxBackoff)。
const (
	outboxBaseBackoff = 30 * time.Second
	outboxMaxBackoff  = time.Hour
)

// NotificationOutboxWorker は event_notification_outbox テーブルの未送信行を処理し、
// 通知メールを送信する。
//
// 本番では Cloud Scheduler からの呼び出しごとに ProcessOnce を 1 回実行する（ADR-0032、ADR-0034）。
// ローカル開発では Run が一定周期で ProcessOnce を呼ぶ。複数の処理が同時に走っても、
// 行の確保は repository が `FOR UPDATE SKIP LOCKED` で排他する。
type NotificationOutboxWorker struct {
	outboxRepo repository.EventNotificationOutboxRepository
	joinRepo   repository.EventJoinRepository
	// eventRepo は organizer 宛通知の宛先（主催者メールアドレス）解決に使う（ADR-0031）。
	eventRepo repository.EventRepository
	mailer    Mailer

	// budget は outboxProcessBudget、markReserve は outboxMarkReserve。テストで差し替えられるようにフィールドで持つ。
	budget      time.Duration
	markReserve time.Duration
}

// NewNotificationOutboxWorker は NotificationOutboxWorker を生成する。
func NewNotificationOutboxWorker(
	outboxRepo repository.EventNotificationOutboxRepository,
	joinRepo repository.EventJoinRepository,
	eventRepo repository.EventRepository,
	mailer Mailer,
) *NotificationOutboxWorker {
	return &NotificationOutboxWorker{
		outboxRepo:  outboxRepo,
		joinRepo:    joinRepo,
		eventRepo:   eventRepo,
		mailer:      mailer,
		budget:      outboxProcessBudget,
		markReserve: outboxMarkReserve,
	}
}

// Run は一定周期で ProcessOnce を呼び続ける（ローカル開発用）。ctx がキャンセルされるまで
// 動き続ける（graceful shutdown はシグナルの ctx を渡すことで自然に停止する）。
//
// ctx がキャンセルされると、新しい outbox 行への着手を止めて速やかに return する。
// ただし着手済みの1件については、送信結果の後始末（MarkSent 等）を
// context.WithoutCancel で完了させてから戻る（processOne 参照）。
func (w *NotificationOutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	// 起動直後にも1回処理し、起動前に溜まっていた通知を待たせない。
	w.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// runOnce は ProcessOnce を呼び、失敗をログに残す。
func (w *NotificationOutboxWorker) runOnce(ctx context.Context) {
	if _, err := w.ProcessOnce(ctx); err != nil {
		slog.Error("outbox の処理に失敗しました", slog.Any("error", err))
	}
}

// ProcessOnce は送信対象の outbox 行を最大 outboxBatchSize 件まで確保して順に処理し、
// 処理した件数を返す。個々の行の失敗は他の行の処理を止めない（slog に記録して次の行へ進む）。
//
// 行の確保に失敗したときは、それまでに処理した件数とエラーを返す。
//
// 開始から budget を過ぎた後、または ctx がキャンセルされた後（シャットダウン中）は、
// 新しい行に着手せずに返す。着手中の 1 件は送信結果の記録まで行う。記録できずに
// pending のまま残った行は、次回の呼び出しで再送される（at-least-once）。
func (w *NotificationOutboxWorker) ProcessOnce(ctx context.Context) (int, error) {
	var handled []uuid.UUID
	deadline := time.Now().Add(w.budget)

	for len(handled) < outboxBatchSize {
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return len(handled), nil
		}

		claim, err := w.outboxRepo.ClaimNextDue(ctx, time.Now(), handled)
		if err != nil {
			return len(handled), fmt.Errorf("claim outbox: %w", err)
		}
		if claim == nil {
			return len(handled), nil
		}

		handled = append(handled, claim.Item().ID)
		w.processClaim(ctx, claim)
	}
	return len(handled), nil
}

// processClaim は確保した 1 行を処理し、panic した場合も含めて必ず Release する。
func (w *NotificationOutboxWorker) processClaim(ctx context.Context, claim repository.OutboxClaim) {
	defer func() {
		if err := claim.Release(); err != nil {
			slog.Error("outbox の解放に失敗しました",
				slog.String("outbox_id", claim.Item().ID.String()),
				slog.Any("error", err),
			)
		}
	}()
	w.processOne(ctx, claim)
}

// processOne は outbox 1行分の送信を試みる。
//
// recipient_kind が organizer の行は主催者1人へ1通だけ送り、members の行は参加者全員へ
// 送る（ADR-0031）。参加者が0件の場合は送信不要のため sent 扱いにする。送信に成功すれば
// sent、失敗すれば試行回数に応じて retry（次回試行日時を予約）または failed（最大試行回数
// 到達）を記録する。
//
// 宛先取得（GetOrganizerEmail / ListRecipients）とメール送信（SendBatch）は workCtx で行う。
// workCtx は ctx に従い（シャットダウンで中断される）、確保の期限の markReserve 前に期限切れになる。
// 中断・期限切れの送信は失敗として retry（または failed）に記録される。宛先取得が中断された行は
// pending のまま残り、次回の呼び出しで再試行される。
//
// 送信結果の後始末（MarkSent/MarkRetry/MarkFailed）は markCtx で行う。markCtx は ctx のキャンセルを
// 引き継がず（context.WithoutCancel）、確保の期限で期限切れになる。SendBatch 成功後に ctx が
// キャンセルされていても MarkSent を完了させる。完了しなければ、送信成功した行が pending のまま残り、
// 次回の呼び出しで再送される。
func (w *NotificationOutboxWorker) processOne(ctx context.Context, claim repository.OutboxClaim) {
	item := claim.Item()

	// context.WithoutCancel は親の値（slog 用のコンテキスト値等）は引き継ぎつつ、
	// キャンセル・デッドラインだけを外す。
	markCtx, cancelMark := context.WithDeadline(context.WithoutCancel(ctx), claim.Deadline())
	defer cancelMark()
	workCtx, cancelWork := context.WithDeadline(ctx, claim.Deadline().Add(-w.markReserve))
	defer cancelWork()

	if item.RecipientKind == model.NotificationOutboxRecipientKindOrganizer {
		w.processOrganizer(workCtx, markCtx, claim)
		return
	}

	recipients, err := w.joinRepo.ListRecipients(workCtx, item.EventID)
	if err != nil {
		slog.Error("outbox の宛先取得に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	if len(recipients) == 0 {
		if err := claim.MarkSent(markCtx); err != nil {
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

	if err := w.mailer.SendBatch(workCtx, emails); err != nil {
		w.handleSendFailure(markCtx, claim, err)
		return
	}

	if err := claim.MarkSent(markCtx); err != nil {
		slog.Error("outbox の sent 更新に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
	}
}

// processOrganizer は主催者宛の outbox 1行分の送信を試みる（ADR-0031）。
// 主催者のメールアドレスは eventRepo.GetOrganizerEmail で解決し、1通だけ送信する
// （ADR-0004 の個別送信）。解決できない場合は pending のまま残し、次回の呼び出しで再試行する。
func (w *NotificationOutboxWorker) processOrganizer(ctx context.Context, markCtx context.Context, claim repository.OutboxClaim) {
	item := claim.Item()

	to, err := w.eventRepo.GetOrganizerEmail(ctx, item.EventID)
	if err != nil {
		slog.Error("outbox の宛先取得に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	emails := []Email{{To: to, Subject: item.Subject, Text: item.Body}}
	if err := w.mailer.SendBatch(ctx, emails); err != nil {
		w.handleSendFailure(markCtx, claim, err)
		return
	}

	if err := claim.MarkSent(markCtx); err != nil {
		slog.Error("outbox の sent 更新に失敗しました",
			slog.String("outbox_id", item.ID.String()),
			slog.Any("error", err),
		)
	}
}

// handleSendFailure は送信失敗時の後処理を行う。
// 更新後の試行回数が outboxMaxAttempts 以上なら failed、未満なら
// 指数バックオフで次回試行日時を予約した retry を記録する。
//
// ctx には markCtx（processOne 参照）を渡すこと。シャットダウン中の送信失敗でも
// 後始末（MarkRetry/MarkFailed）を完了させる。
func (w *NotificationOutboxWorker) handleSendFailure(ctx context.Context, claim repository.OutboxClaim, sendErr error) {
	item := claim.Item()
	nextAttempts := item.Attempts + 1

	if nextAttempts >= outboxMaxAttempts {
		if err := claim.MarkFailed(ctx, sendErr.Error()); err != nil {
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
	if err := claim.MarkRetry(ctx, nextAttemptAt, sendErr.Error()); err != nil {
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
