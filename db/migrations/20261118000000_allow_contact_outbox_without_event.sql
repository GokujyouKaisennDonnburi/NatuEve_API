-- +goose Up
-- お問い合わせはイベントに紐づかないemail宛通知として保存する（ADR-0052）。
ALTER TABLE event_notification_outbox ALTER COLUMN event_id DROP NOT NULL;
ALTER TABLE event_notification_outbox
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'event';
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
-- お問い合わせ行は送信状態を問わず削除し、イベント通知は残す（ADR-0052）。
DELETE FROM event_notification_outbox WHERE purpose = 'contact';
DROP INDEX idx_event_notification_outbox_pending_priority;
DROP INDEX idx_event_notification_outbox_contact_created;
ALTER TABLE event_notification_outbox
    DROP CONSTRAINT event_notification_outbox_purpose_check;
ALTER TABLE event_notification_outbox DROP COLUMN purpose;
ALTER TABLE event_notification_outbox ALTER COLUMN event_id SET NOT NULL;
