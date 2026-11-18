package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ContactRepository は管理者宛のお問い合わせメールをoutboxに予約する。
type ContactRepository interface {
	Enqueue(ctx context.Context, recipient, subject, body string, receivedAt time.Time) error
}

type contactPostgres struct {
	db *sql.DB
}

// NewContactRepository はDBを使うお問い合わせRepositoryを生成する。
func NewContactRepository(db *sql.DB) ContactRepository {
	return &contactPostgres{db: db}
}

func (r *contactPostgres) Enqueue(ctx context.Context, recipient, subject, body string, receivedAt time.Time) error {
	const query = `
		INSERT INTO event_notification_outbox
			(event_id, recipient_kind, recipient_email, subject, body, created_at)
		VALUES (NULL, $1, $2, $3, $4, $5)`
	if _, err := r.db.ExecContext(ctx, query,
		model.NotificationOutboxRecipientKindEmail, recipient, subject, body, receivedAt,
	); err != nil {
		return fmt.Errorf("enqueue contact: %w", err)
	}
	return nil
}
