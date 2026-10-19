package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// fakeOutboxRepo は EventNotificationOutboxRepository のテスト用フェイク。
// ClaimNextDue が返す OutboxClaim 経由の MarkSent/MarkRetry/MarkFailed/Release を記録する。
type fakeOutboxRepo struct {
	dueItems []model.EventNotificationOutbox
	claimErr error

	// claimDeadline は確保した行の Deadline。ゼロ値なら確保時点から 1 時間後。
	claimDeadline time.Time

	sentIDs     []uuid.UUID
	retryCalls  []outboxRetryCall
	failedCalls []outboxFailedCall
	released    int

	// lastMarkSentCtxErr は直近の MarkSent 呼び出し時点での ctx.Err() を記録する。
	lastMarkSentCtxErr error
	// lastMarkRetryCtxErr は直近の MarkRetry 呼び出し時点での ctx.Err() を記録する。
	lastMarkRetryCtxErr error
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

// ClaimNextDue は dueItems のうち excludeIDs に無い先頭の 1 件を確保して返す。
// Mark* で確定した行は dueItems から外れる（実 DB で pending でなくなるのと同じ）。
func (f *fakeOutboxRepo) ClaimNextDue(_ context.Context, _ time.Time, excludeIDs []uuid.UUID) (repository.OutboxClaim, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	for _, item := range f.dueItems {
		if !slices.Contains(excludeIDs, item.ID) {
			return f.claim(item), nil
		}
	}
	return nil, nil
}

// claim は processOne を直接呼ぶテスト用に、指定の行を確保した状態の OutboxClaim を作る。
func (f *fakeOutboxRepo) claim(item model.EventNotificationOutbox) repository.OutboxClaim {
	deadline := f.claimDeadline
	if deadline.IsZero() {
		deadline = time.Now().Add(time.Hour)
	}
	return &fakeClaim{repo: f, item: item, deadline: deadline}
}

// fakeClaim は repository.OutboxClaim のテスト用フェイク。
type fakeClaim struct {
	repo     *fakeOutboxRepo
	item     model.EventNotificationOutbox
	deadline time.Time
}

func (c *fakeClaim) Item() model.EventNotificationOutbox { return c.item }

func (c *fakeClaim) Deadline() time.Time { return c.deadline }

func (c *fakeClaim) MarkSent(ctx context.Context) error {
	c.repo.sentIDs = append(c.repo.sentIDs, c.item.ID)
	// シャットダウン挙動のテスト用に、呼び出し時点の ctx の状態を記録する。
	// 実 DB 実装ではキャンセル済み ctx を渡すとクエリが失敗するため、
	// MarkSent がキャンセルされていない ctx（markCtx）で呼ばれたことを検証できる。
	c.repo.lastMarkSentCtxErr = ctx.Err()
	c.repo.settle(c.item.ID)
	return nil
}

func (c *fakeClaim) MarkRetry(ctx context.Context, nextAttemptAt time.Time, lastError string) error {
	c.repo.lastMarkRetryCtxErr = ctx.Err()
	c.repo.retryCalls = append(c.repo.retryCalls, outboxRetryCall{id: c.item.ID, nextAttemptAt: nextAttemptAt, lastError: lastError})
	c.repo.settle(c.item.ID)
	return nil
}

func (c *fakeClaim) MarkFailed(_ context.Context, lastError string) error {
	c.repo.failedCalls = append(c.repo.failedCalls, outboxFailedCall{id: c.item.ID, lastError: lastError})
	c.repo.settle(c.item.ID)
	return nil
}

func (c *fakeClaim) Release() error {
	c.repo.released++
	return nil
}

// settle は確定した行を dueItems から外す。
func (f *fakeOutboxRepo) settle(id uuid.UUID) {
	f.dueItems = slices.DeleteFunc(f.dueItems, func(i model.EventNotificationOutbox) bool { return i.ID == id })
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

func (f *fakeWorkerJoinRepo) Absence(context.Context, uuid.UUID, uuid.UUID, string, string, string, string) (time.Time, error) {
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

func (f *fakeWorkerJoinRepo) GetMemberByProfile(context.Context, uuid.UUID, uuid.UUID) (model.EventMember, error) {
	panic("not implemented in worker test fake")
}

// fakeWorkerEventRepo は EventRepository のテスト用フェイク。
// worker のテストで使うのは GetOrganizerEmail のみだが、interface を満たすため
// 他メソッドは未実装（呼ばれたら fail する）のダミーを用意する。
type fakeWorkerEventRepo struct {
	organizerEmailByEvent map[uuid.UUID]string
	organizerEmailErr     error
}

func (f *fakeWorkerEventRepo) GetOrganizerEmail(_ context.Context, eventID uuid.UUID) (string, error) {
	if f.organizerEmailErr != nil {
		return "", f.organizerEmailErr
	}
	return f.organizerEmailByEvent[eventID], nil
}

func (f *fakeWorkerEventRepo) ListSummaries(context.Context, string, string, int, int) ([]model.EventSummary, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) CountSummaries(context.Context) (int, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) SearchSummaries(context.Context, model.EventSearchFilter, string, string, int, int) ([]model.EventSummary, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) CountSearchSummaries(context.Context, model.EventSearchFilter) (int, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) ListMySummaries(context.Context, model.MyEventFilter, string, string, int, int) ([]model.EventSummary, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) CountMyEventKinds(context.Context, uuid.UUID) (model.MyEventCounts, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) GetByID(context.Context, string) (*model.EventResponse, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) Create(context.Context, *model.NewEvent) (model.CreateEventResponse, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) GetOwnerProfileID(context.Context, string) (string, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) GetTitle(context.Context, string) (string, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) Exists(context.Context, uuid.UUID) (bool, error) {
	panic("not implemented in worker test fake")
}

func (f *fakeWorkerEventRepo) CancelWithNotification(context.Context, uuid.UUID, string, string) (time.Time, error) {
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

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

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

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

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
		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

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

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

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

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		if _, err := w.ProcessOnce(context.Background()); err != nil {
			t.Fatalf("ProcessOnce() returned error: %v", err)
		}

		if len(outboxRepo.sentIDs) != 2 {
			t.Fatalf("sentIDs = %v, want 2件とも sent", outboxRepo.sentIDs)
		}
	})
}

func TestNotificationOutboxWorker_ProcessOneOrganizer(t *testing.T) {
	eventID := uuid.New()

	// organizer 宛の outbox 行を組み立てる。
	newOrganizerItem := func() model.EventNotificationOutbox {
		return model.EventNotificationOutbox{
			ID:            uuid.New(),
			EventID:       eventID,
			RecipientKind: model.NotificationOutboxRecipientKindOrganizer,
			Subject:       "欠席連絡の件名",
			Body:          "欠席連絡の本文",
			Attempts:      0,
		}
	}

	t.Run("正常: organizer 宛は主催者1人へ1通だけ送信し MarkSent が呼ばれる", func(t *testing.T) {
		item := newOrganizerItem()
		outboxRepo := &fakeOutboxRepo{}
		// members 経路（ListRecipients）に宛先を残しておき、organizer 経路では
		// 使われないことを送信宛先の比較で確認する。
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{
			eventID: {{MailAddress: "member@example.com"}},
		}}
		eventRepo := &fakeWorkerEventRepo{organizerEmailByEvent: map[uuid.UUID]string{
			eventID: "organizer@example.com",
		}}
		mailer := &workerFakeMailer{}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, eventRepo, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

		if mailer.callCount != 1 {
			t.Fatalf("SendBatch call count = %d, want 1", mailer.callCount)
		}
		if len(mailer.gotEmails) != 1 {
			t.Fatalf("SendBatch emails = %d, want 1（主催者1人だけに送る）", len(mailer.gotEmails))
		}
		if mailer.gotEmails[0].To != "organizer@example.com" {
			t.Errorf("SendBatch to = %q, want %q（ListRecipients の宛先を使わないこと）",
				mailer.gotEmails[0].To, "organizer@example.com")
		}
		if mailer.gotEmails[0].Subject != item.Subject {
			t.Errorf("SendBatch subject = %q, want %q", mailer.gotEmails[0].Subject, item.Subject)
		}
		if mailer.gotEmails[0].Text != item.Body {
			t.Errorf("SendBatch text = %q, want %q", mailer.gotEmails[0].Text, item.Body)
		}
		if len(outboxRepo.sentIDs) != 1 || outboxRepo.sentIDs[0] != item.ID {
			t.Errorf("sentIDs = %v, want [%v]", outboxRepo.sentIDs, item.ID)
		}
	})

	t.Run("異常: 主催者のメールアドレス解決に失敗したら送信せず pending のまま残る", func(t *testing.T) {
		item := newOrganizerItem()
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{}
		eventRepo := &fakeWorkerEventRepo{organizerEmailErr: errors.New("db error")}
		mailer := &workerFakeMailer{}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, eventRepo, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

		if mailer.callCount != 0 {
			t.Errorf("SendBatch call count = %d, want 0", mailer.callCount)
		}
		if len(outboxRepo.sentIDs) != 0 || len(outboxRepo.retryCalls) != 0 || len(outboxRepo.failedCalls) != 0 {
			t.Errorf("sent/retry/failed は更新されない（pending のまま再試行）: sent=%v retry=%v failed=%v",
				outboxRepo.sentIDs, outboxRepo.retryCalls, outboxRepo.failedCalls)
		}
	})

	t.Run("異常: organizer 宛の送信失敗（最大試行回数未満）は MarkRetry が呼ばれる", func(t *testing.T) {
		item := newOrganizerItem()
		item.Attempts = 2
		outboxRepo := &fakeOutboxRepo{}
		joinRepo := &fakeWorkerJoinRepo{}
		eventRepo := &fakeWorkerEventRepo{organizerEmailByEvent: map[uuid.UUID]string{
			eventID: "organizer@example.com",
		}}
		sendErr := errors.New("resend: temporary failure")
		mailer := &workerFakeMailer{sendErr: sendErr}

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, eventRepo, mailer)
		w.processOne(context.Background(), outboxRepo.claim(item))

		if len(outboxRepo.retryCalls) != 1 {
			t.Fatalf("retryCalls = %d, want 1", len(outboxRepo.retryCalls))
		}
		if outboxRepo.retryCalls[0].id != item.ID {
			t.Errorf("retry call id = %v, want %v", outboxRepo.retryCalls[0].id, item.ID)
		}
		if outboxRepo.retryCalls[0].lastError != sendErr.Error() {
			t.Errorf("retry call lastError = %q, want %q", outboxRepo.retryCalls[0].lastError, sendErr.Error())
		}
		if len(outboxRepo.sentIDs) != 0 || len(outboxRepo.failedCalls) != 0 {
			t.Errorf("sent/failed は更新されない: sent=%v failed=%v", outboxRepo.sentIDs, outboxRepo.failedCalls)
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

	t.Run("ProcessOnce はキャンセル済み ctx では新しい行の処理に着手しない", func(t *testing.T) {
		item := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{item}}
		joinRepo := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{eventID: recipients}}
		mailer := &workerFakeMailer{}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // シャットダウン中を模す

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		n, err := w.ProcessOnce(ctx)
		if err != nil || n != 0 {
			t.Errorf("ProcessOnce() = (%d, %v), want (0, nil)", n, err)
		}

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

		w := NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, mailer)
		w.processOne(ctx, outboxRepo.claim(item))

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

func TestNotificationOutboxWorker_ProcessOnce(t *testing.T) {
	eventID := uuid.New()
	newWorker := func(outboxRepo *fakeOutboxRepo, joinRepo *fakeWorkerJoinRepo) *NotificationOutboxWorker {
		return NewNotificationOutboxWorker(outboxRepo, joinRepo, &fakeWorkerEventRepo{}, &workerFakeMailer{})
	}
	noRecipients := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{}}

	t.Run("正常: 対象が0件でもエラーにならず 0 を返す", func(t *testing.T) {
		outboxRepo := &fakeOutboxRepo{}
		n, err := newWorker(outboxRepo, noRecipients).ProcessOnce(context.Background())
		if err != nil || n != 0 {
			t.Fatalf("ProcessOnce() = (%d, %v), want (0, nil)", n, err)
		}
	})

	t.Run("正常: 処理した件数を返し、確保した行を毎回解放する", func(t *testing.T) {
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{
			{ID: uuid.New(), EventID: eventID},
			{ID: uuid.New(), EventID: eventID},
			{ID: uuid.New(), EventID: eventID},
		}}
		n, err := newWorker(outboxRepo, noRecipients).ProcessOnce(context.Background())
		if err != nil || n != 3 {
			t.Fatalf("ProcessOnce() = (%d, %v), want (3, nil)", n, err)
		}
		if outboxRepo.released != 3 {
			t.Errorf("Release 呼び出し回数 = %d, want 3", outboxRepo.released)
		}
	})

	t.Run("正常: 確定されずに残った行を同じ呼び出しで取り直さない", func(t *testing.T) {
		// 宛先取得に失敗した行は Mark* されず pending のまま残る。
		// 取り直すと同じ行を何度も処理してしまうため、excludeIDs で除外されることを確認する。
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{{ID: uuid.New(), EventID: eventID}}}
		joinRepo := &fakeWorkerJoinRepo{recipientsErr: errors.New("db error")}
		n, err := newWorker(outboxRepo, joinRepo).ProcessOnce(context.Background())
		if err != nil || n != 1 {
			t.Fatalf("ProcessOnce() = (%d, %v), want (1, nil)", n, err)
		}
	})

	t.Run("正常: 1 回の呼び出しで処理する件数は outboxBatchSize まで", func(t *testing.T) {
		items := make([]model.EventNotificationOutbox, 0, outboxBatchSize+5)
		for range outboxBatchSize + 5 {
			items = append(items, model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID})
		}
		outboxRepo := &fakeOutboxRepo{dueItems: items}
		n, err := newWorker(outboxRepo, noRecipients).ProcessOnce(context.Background())
		if err != nil || n != outboxBatchSize {
			t.Fatalf("ProcessOnce() = (%d, %v), want (%d, nil)", n, err, outboxBatchSize)
		}
	})

	t.Run("正常: 時間予算を過ぎたら新しい行を確保しない", func(t *testing.T) {
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{{ID: uuid.New(), EventID: eventID}}}
		w := newWorker(outboxRepo, noRecipients)
		w.budget = 0
		n, err := w.ProcessOnce(context.Background())
		if err != nil || n != 0 {
			t.Fatalf("ProcessOnce() = (%d, %v), want (0, nil)", n, err)
		}
		if len(outboxRepo.sentIDs) != 0 {
			t.Errorf("sentIDs = %v, want 空", outboxRepo.sentIDs)
		}
	})

	t.Run("異常: 処理中に panic しても確保した行は解放される", func(t *testing.T) {
		outboxRepo := &fakeOutboxRepo{dueItems: []model.EventNotificationOutbox{{ID: uuid.New(), EventID: eventID}}}
		w := NewNotificationOutboxWorker(outboxRepo, noRecipients, &fakeWorkerEventRepo{}, panicMailer{})
		// 参加者0件だと送信されないため、宛先を1件にして SendBatch まで進める。
		w.joinRepo = &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{
			eventID: {{MailAddress: "yamada@example.com"}},
		}}

		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("panic が伝播しなかった")
				}
			}()
			_, _ = w.ProcessOnce(context.Background())
		}()
		if outboxRepo.released != 1 {
			t.Errorf("Release 呼び出し回数 = %d, want 1", outboxRepo.released)
		}
	})

	t.Run("異常: 行の確保に失敗したらエラーを返す", func(t *testing.T) {
		outboxRepo := &fakeOutboxRepo{claimErr: errors.New("db down")}
		n, err := newWorker(outboxRepo, noRecipients).ProcessOnce(context.Background())
		if err == nil || n != 0 {
			t.Fatalf("ProcessOnce() = (%d, %v), want (0, error)", n, err)
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

// panicMailer は SendBatch で panic する Mailer のテスト用フェイク。
type panicMailer struct{}

func (panicMailer) SendBatch(context.Context, []Email) error {
	panic("mailer panic")
}

func TestNotificationOutboxWorker_ClaimDeadline(t *testing.T) {
	eventID := uuid.New()
	recipients := &fakeWorkerJoinRepo{recipientsByEvent: map[uuid.UUID][]model.EventRecipient{
		eventID: {{MailAddress: "yamada@example.com"}},
	}}

	t.Run("送信に markReserve より長くかかっても、MarkSent の ctx は期限切れにならない", func(t *testing.T) {
		item := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		outboxRepo := &fakeOutboxRepo{}
		w := NewNotificationOutboxWorker(outboxRepo, recipients, &fakeWorkerEventRepo{}, &slowMailer{delay: 100 * time.Millisecond})
		w.markReserve = 50 * time.Millisecond

		w.processOne(context.Background(), outboxRepo.claim(item))

		if len(outboxRepo.sentIDs) != 1 {
			t.Fatalf("sentIDs = %v, want 1件", outboxRepo.sentIDs)
		}
		if outboxRepo.lastMarkSentCtxErr != nil {
			t.Errorf("MarkSent に渡された ctx.Err() = %v, want nil", outboxRepo.lastMarkSentCtxErr)
		}
	})

	t.Run("確保の期限の markReserve 前を過ぎた送信は打ち切られ、期限内に retry が記録される", func(t *testing.T) {
		item := model.EventNotificationOutbox{ID: uuid.New(), EventID: eventID}
		outboxRepo := &fakeOutboxRepo{claimDeadline: time.Now().Add(300 * time.Millisecond)}
		mailer := &blockingMailer{}
		w := NewNotificationOutboxWorker(outboxRepo, recipients, &fakeWorkerEventRepo{}, mailer)
		w.markReserve = 250 * time.Millisecond

		w.processOne(context.Background(), outboxRepo.claim(item))

		if !errors.Is(mailer.gotErr, context.DeadlineExceeded) {
			t.Errorf("SendBatch の ctx.Err() = %v, want DeadlineExceeded", mailer.gotErr)
		}
		if len(outboxRepo.retryCalls) != 1 {
			t.Fatalf("retryCalls = %v, want 1件", outboxRepo.retryCalls)
		}
		if outboxRepo.lastMarkRetryCtxErr != nil {
			t.Errorf("MarkRetry に渡された ctx.Err() = %v, want nil（確保の期限内に記録する）", outboxRepo.lastMarkRetryCtxErr)
		}
	})
}

// slowMailer は delay だけ待ってから成功を返す Mailer のテスト用フェイク。
type slowMailer struct {
	delay time.Duration
}

func (m *slowMailer) SendBatch(context.Context, []Email) error {
	time.Sleep(m.delay)
	return nil
}

// blockingMailer は ctx が終わるまで待ち、ctx.Err() を返す Mailer のテスト用フェイク。
type blockingMailer struct {
	gotErr error
}

func (m *blockingMailer) SendBatch(ctx context.Context, _ []Email) error {
	<-ctx.Done()
	m.gotErr = ctx.Err()
	return m.gotErr
}
