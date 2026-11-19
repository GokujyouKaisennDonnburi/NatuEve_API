-- +goose Up
ALTER TABLE event_notification_outbox
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'event';

-- この移行前にイベントIDなしで保存された行はお問い合わせ（ADR-0052）。
UPDATE event_notification_outbox SET purpose = 'contact' WHERE event_id IS NULL;

ALTER TABLE event_notification_outbox
    DROP CONSTRAINT event_notification_outbox_event_required_check;
ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_purpose_check CHECK (
        (purpose = 'event' AND event_id IS NOT NULL)
        OR (purpose = 'contact' AND event_id IS NULL AND recipient_kind = 'email')
    );

CREATE INDEX idx_event_notification_outbox_contact_created
    ON event_notification_outbox (created_at) WHERE purpose = 'contact';
CREATE INDEX idx_event_notification_outbox_pending_priority
    ON event_notification_outbox ((purpose = 'contact'), next_attempt_at)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX idx_event_notification_outbox_pending_priority;
DROP INDEX idx_event_notification_outbox_contact_created;
ALTER TABLE event_notification_outbox
    DROP CONSTRAINT event_notification_outbox_purpose_check;
ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_event_required_check
    CHECK (event_id IS NOT NULL OR recipient_kind = 'email');
ALTER TABLE event_notification_outbox DROP COLUMN purpose;
