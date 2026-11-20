package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ErrContactLimitReached はお問い合わせ全体の受付上限に達したことを表す。
var ErrContactLimitReached = errors.New("contact limit reached")

// ContactRepository は管理者宛のお問い合わせメールをoutboxに予約する。
type ContactRepository interface {
	// Enqueue は全IP共通のwindow内の受付件数を確認し、上限未満なら同じトランザクションで保存する。
	// 上限到達時はErrContactLimitReachedを返し、行を追加しない。
	Enqueue(ctx context.Context, recipient, subject, body string, receivedAt time.Time, window time.Duration, limit int) error
}

type contactPostgres struct {
	db *sql.DB
}

// NewContactRepository はDBを使うお問い合わせRepositoryを生成する。
func NewContactRepository(db *sql.DB) ContactRepository {
	return &contactPostgres{db: db}
}

func (r *contactPostgres) Enqueue(ctx context.Context, recipient, subject, body string, receivedAt time.Time, window time.Duration, limit int) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin enqueue contact: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 共通キーで件数確認とINSERTを直列化する。ロックはトランザクション終了時に解放される。
	const lockQuery = `SELECT pg_advisory_xact_lock(hashtextextended('natueve:contact:admission', 0))`
	if _, err := tx.ExecContext(ctx, lockQuery); err != nil {
		return fmt.Errorf("lock contact counter: %w", err)
	}
	var count int
	const countQuery = `SELECT count(*) FROM event_notification_outbox
		WHERE purpose = $1 AND created_at > $2`
	if err := tx.QueryRowContext(ctx, countQuery, model.NotificationOutboxPurposeContact, receivedAt.Add(-window)).Scan(&count); err != nil {
		return fmt.Errorf("count contacts: %w", err)
	}
	if count >= limit {
		return ErrContactLimitReached
	}
	const query = `
		INSERT INTO event_notification_outbox
			(event_id, recipient_kind, recipient_email, subject, body, created_at, purpose)
		VALUES (NULL, $1, $2, $3, $4, $5, $6)`
	if _, err := tx.ExecContext(ctx, query,
		model.NotificationOutboxRecipientKindEmail, recipient, subject, body, receivedAt, model.NotificationOutboxPurposeContact,
	); err != nil {
		return fmt.Errorf("enqueue contact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit contact: %w", err)
	}
	return nil
}
