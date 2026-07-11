package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// fakeOutboxRepo は EventNotificationOutboxRepository のテスト用フェイク。
// MarkSent/MarkRetry/MarkFailed への呼び出しを記録する。
type fakeOutboxRepo struct {
	dueItems   []model.EventNotificationOutbox
	listDueErr error

	sentIDs     []uuid.UUID
	retryCalls  []outboxRetryCall
	failedCalls []outboxFailedCall

	// lastMarkSentCtxErr は直近の MarkSent 呼び出し時点での ctx.Err() を記録する。
	lastMarkSentCtxErr error
}

type outboxRetryCall struct {
	id            uuid.UUID
	nextAttemptAt time.Time
	lastError     string
}

type outboxFailedCall struct {
	id        uuid.UUID
	lastError string
}

func (f *fakeOutboxRepo) ListDue(_ context.Context, _ time.Time, _ int) ([]model.EventNotificationOutbox, error) {
	return f.dueItems, f.listDueErr
}

func (f *fakeOutboxRepo) MarkSent(ctx context.Context, id uuid.UUID) error {
	f.sentIDs = append(f.sentIDs, id)
	// シャットダウン挙動のテスト用に、呼び出し時点の ctx の状態を記録する。
	// 実 DB 実装ではキャンセル済み ctx を渡すとクエリが失敗するため、
	// MarkSent がキャンセルされていない ctx（markCtx）で呼ばれたことを検証できる。
	f.lastMarkSentCtxErr = ctx.Err()
	return nil
}

func (f *fakeOutboxRepo) MarkRetry(_ context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error {
	f.retryCalls = append(f.retryCalls, outboxRetryCall{id: id, nextAttemptAt: nextAttemptAt, lastError: lastError})
	return nil
}

func (f *fakeOutboxRepo) MarkFailed(_ context.Context, id uuid.UUID, lastError string) error {
	f.failedCalls = append(f.failedCalls, outboxFailedCall{id: id, lastError: lastError})
	return nil
}

// fakeWorkerJoinRepo は EventJoinRepository のテスト用フェイク。
// worker のテストで使うのは ListRecipients のみだが、interface を満たすため
// 他メソッドは未実装（呼ばれたら fail する）のダミーを用意する。
type fakeWorkerJoinRepo struct {
	recipientsByEvent map[uuid.UUID][]model.EventRecipient
	recipientsErr     error
}

func (f *fakeWorkerJoinRepo) Join(context.Context, *model.EventMember) error {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerJoinRepo) Leave(context.Context, uuid.UUID, uuid.UUID) (time.Time, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerJoinRepo) ListRecipients(_ context.Context, eventID uuid.UUID) ([]model.EventRecipient, error) {
	if f.recipientsErr != nil {
		return nil, f.recipientsErr
	}
	return f.recipientsByEvent[eventID], nil
}

func (f *fakeWorkerJoinRepo) ListMembers(context.Context, uuid.UUID) ([]model.EventMember, error) {
	panic("not implemented in worker test fake")
}

// workerFakeMailer は Mailer のテスト用フェイク。SendBatch の成否を差し替えられる。
type workerFakeMailer struct {
	sendErr error
	// gotEmails は最後に呼ばれた SendBatch の引数を記録する。
	gotEmails []Email
	callCount int
}

func (m *workerFakeMailer) SendBatch(_ context.Context, emails []Email) error {
	m.callCount++
	m.gotEmails = emails
	return m.sendErr
}

func TestNotificationOutboxWorker_ProcessOne(t *testing.T) {
	eventID := uuid.New()
	recipients := []model.EventRecipient{
		{MailAddress: "yamada@example.com"},
		{MailAddress: "sato@example.com"},
	}

	t.Run("正常: 送信成功で MarkSent が呼ばれる", func(t *testing.T) {
		item := model.EventNotificationOutbox{
			ID:       uuid.New(),
			EventID:  eventID,
			Subject:  "件名",
			Body:     "本文",
			Attempts: 0,
		}
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}
		mailer := &workerFakeMailer{}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processOne(context.Background(), item)

		if mailer.callCount != 1 {
			t.Fatalf("SendBatch call count = %d, want 1", mailer.callCount)
		}
		if len(mailer.gotEmails) != 2 {
			t.Fatalf("SendBatch emails = %d, want 2", len(mailer.gotEmails))
		}
		if len(outboxRepo.sentIDs) != 1 || outboxRepo.sentIDs[0] != item.ID {
			t.Errorf("sentIDs = %v, want [%v]", outboxRepo.sentIDs, item.ID)
		}
		if len(outboxRepo.retryCalls) != 0 || len(outboxRepo.failedCalls) != 0 {
			t.Errorf("retryCalls/failedCalls should be empty, got retry=%v failed=%v", outboxRepo.retryCalls, outboxRepo.failedCalls)
		}
	})

	t.Run("正常: 参加者0件なら送信せず MarkSent が呼ばれる", func(t *testing.T) {
		item := model.EventNotificationOutbox{
			ID:      uuid.New(),
			EventID: eventID,
		}
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{}}
		mailer := &workerFakeMailer{}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processOne(context.Background(), item)

		if mailer.callCount != 0 {
			t.Errorf("SendBatch call count = %d, want 0（宛先0件は送信しない）", mailer.callCount)
		}
		if len(outboxRepo.sentIDs) != 1 || outboxRepo.sentIDs[0] != item.ID {
			t.Errorf("sentIDs = %v, want [%v]", outboxRepo.sentIDs, item.ID)
		}
	})

	t.Run("異常: 送信失敗（最大試行回数未満）は MarkRetry が呼ばれる", func(t *testing.T) {
		item := model.EventNotificationOutbox{
			ID:       uuid.New(),
			EventID:  eventID,
			Attempts: 2, // 3回目の失敗（更新後 attempts=3 < outboxMaxAttempts）
		}
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}
		sendErr := errors.New("resend: temporary failure")
		mailer := &workerFakeMailer{sendErr: sendErr}

		before := time.Now()
		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processOne(context.Background(), item)

		if len(outboxRepo.sentIDs) != 0 {
			t.Errorf("sentIDs should be empty on failure, got %v", outboxRepo.sentIDs)
		}
		if len(outboxRepo.failedCalls) != 0 {
			t.Errorf("failedCalls should be empty (attempts未到達), got %v", outboxRepo.failedCalls)
		}
		if len(outboxRepo.retryCalls) != 1 {
			t.Fatalf("retryCalls = %d, want 1", len(outboxRepo.retryCalls))
		}
		call := outboxRepo.retryCalls[0]
		if call.id != item.ID {
			t.Errorf("retry call id = %v, want %v", call.id, item.ID)
		}
		if call.lastError != sendErr.Error() {
			t.Errorf("retry call lastError = %q, want %q", call.lastError, sendErr.Error())
		}
		// nextAttempts = 3 → backoff = min(30s << 3, 1h) = 240s
		wantBackoff := 240 * time.Second
		gotBackoff := call.nextAttemptAt.Sub(before)
		if gotBackoff < wantBackoff-2*time.Second || gotBackoff > wantBackoff+2*time.Second {
			t.Errorf("next_attempt_at のバックオフ ≈ %v, want ≈ %v", gotBackoff, wantBackoff)
		}
	})

	t.Run("異常: 最大試行回数に到達した送信失敗は MarkFailed が呼ばれる", func(t *testing.T) {
		item := model.EventNotificationOutbox{
			ID:       uuid.New(),
			EventID:  eventID,
			Attempts: outboxMaxAttempts - 1, // 更新後 attempts が outboxMaxAttempts に到達
		}
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}
		sendErr := errors.New("resend: permanent failure")
		mailer := &workerFakeMailer{sendErr: sendErr}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processOne(context.Background(), item)

		if len(outboxRepo.sentIDs) != 0 {
			t.Errorf("sentIDs should be empty on failure, got %v", outboxRepo.sentIDs)
		}
		if len(outboxRepo.retryCalls) != 0 {
			t.Errorf("retryCalls should be empty (最大試行回数到達), got %v", outboxRepo.retryCalls)
		}
		if len(outboxRepo.failedCalls) != 1 {
			t.Fatalf("failedCalls = %d, want 1", len(outboxRepo.failedCalls))
		}
		if outboxRepo.failedCalls[0].id != item.ID {
			t.Errorf("failed call id = %v, want %v", outboxRepo.failedCalls[0].id, item.ID)
		}
		if outboxRepo.failedCalls[0].lastError != sendErr.Error() {
			t.Errorf("failed call lastError = %q, want %q", outboxRepo.failedCalls[0].lastError, sendErr.Error())
		}
	})

	t.Run("異常: 個々の行の失敗が他の行の処理を止めない", func(t *testing.T) {
		okItem := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		ngItem := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID, Attempts: outboxMaxAttempts}

		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{ngItem, okItem}}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{}} // 両方とも参加者0件でsent
		mailer := &workerFakeMailer{}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processDue(context.Background())

		if len(outboxRepo.sentIDs) != 2 {
			t.Fatalf("sentIDs = %v, want 2件とも sent", outboxRepo.sentIDs)
		}
	})
}

// cancelingMailer は SendBatch の実行中に外部から渡された cancel を呼び出す
// テスト用フェイク。「SendBatch 成功後始末（MarkSent）が、SendBatch 実行中に
// ctx がキャンセルされても完了する」ことを検証するために使う。
type cancelingMailer struct {
	cancel    context.CancelFunc
	sendErr   error
	callCount int
}

func (m *cancelingMailer) SendBatch(_ context.Context, _ []Email) error {
	m.callCount++
	// SIGTERM がちょうど送信処理中に届いたことを模す。
	m.cancel()
	return m.sendErr
}

func TestNotificationOutboxWorker_Shutdown(t *testing.T) {
	eventID := uuid.New()
	recipients := []model.EventRecipient{
		{MailAddress: "yamada@example.com"},
	}

	t.Run("processDue はキャンセル済み ctx では新しい行の処理に着手しない", func(t *testing.T) {
		item := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{item}}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}
		mailer := &workerFakeMailer{}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // シャットダウン中を模す

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processDue(ctx)

		if mailer.callCount != 0 {
			t.Errorf("SendBatch call count = %d, want 0（キャンセル済み ctx では新規着手しない）", mailer.callCount)
		}
		if len(outboxRepo.sentIDs) != 0 {
			t.Errorf("sentIDs = %v, want 空（新規着手しない）", outboxRepo.sentIDs)
		}
	})

	t.Run("SendBatch 成功後に ctx がキャンセルされても MarkSent は完了する", func(t *testing.T) {
		item := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}

		ctx, cancel := context.WithCancel(context.Background())
		mailer := &cancelingMailer{cancel: cancel} // SendBatch 実行中に ctx をキャンセルするが成功を返す

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, mailer)
		w.processOne(ctx, item)

		if mailer.callCount != 1 {
			t.Fatalf("SendBatch call count = %d, want 1", mailer.callCount)
		}
		if ctx.Err() == nil {
			t.Fatal("前提条件: SendBatch 実行中に ctx がキャンセルされているはず")
		}
		if len(outboxRepo.sentIDs) != 1 || outboxRepo.sentIDs[0] != item.ID {
			t.Fatalf("sentIDs = %v, want [%v]（親 ctx がキャンセルされていても MarkSent は呼ばれるべき）", outboxRepo.sentIDs, item.ID)
		}
		if outboxRepo.lastMarkSentCtxErr != nil {
			t.Errorf("MarkSent に渡された ctx.Err() = %v, want nil（WithoutCancel でキャンセルの影響を受けないこと）", outboxRepo.lastMarkSentCtxErr)
		}
	})
}

func TestNotificationOutboxWorker_Wake(t *testing.T) {
	t.Run("nil レシーバでも panic しない", func(_ *testing.T) {
		var w *NotificationOutboxWorker
		w.Wake() // panic しないことを確認する
	})

	t.Run("バッファ1の非ブロッキング送信で、連続呼び出しでもブロックしない", func(t *testing.T) {
		w := NewNotificationOutboxWorker(&fakeOutboxRepo{}, &fakeWorkerJoinRepo{}, &workerFakeMailer{})
		done := make(chan struct{})
		go func() {
			w.Wake()
			w.Wake()
			w.Wake()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("Wake() がブロックした")
		}
	})
}

func TestBackoffFor(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 0, want: 30 * time.Second},
		{attempts: 1, want: 60 * time.Second},
		{attempts: 2, want: 120 * time.Second},
		{attempts: 7, want: time.Hour}, // 30s<<7 = 3840s > 1h なので上限に丸める
	}
	for _, tt := range tests {
		got := backoffFor(tt.attempts)
		if got != tt.want {
			t.Errorf("backoffFor(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}
