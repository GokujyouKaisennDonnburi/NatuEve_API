-- +goose Up
-- 参加申込の確認メール・案内メールは、参加者全員ではなく1つのメールアドレスへ送る。
-- outbox の宛先種別に email を追加し、宛先アドレスをスナップショットする（ADR-0044）。
ALTER TABLE event_notification_outbox
    ADD COLUMN recipient_email TEXT;

ALTER TABLE event_notification_outbox
    DROP CONSTRAINT event_notification_outbox_recipient_kind_check;

ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_recipient_kind_check
    CHECK (recipient_kind IN ('members', 'organizer', 'email'));

ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_email_recipient_check
    CHECK (recipient_kind <> 'email' OR recipient_email IS NOT NULL);

-- +goose Down
ALTER TABLE event_notification_outbox
    DROP CONSTRAINT IF EXISTS event_notification_outbox_email_recipient_check;

DELETE FROM event_notification_outbox
WHERE recipient_kind = 'email';

ALTER TABLE event_notification_outbox
    DROP CONSTRAINT IF EXISTS event_notification_outbox_recipient_kind_check;

ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_recipient_kind_check
    CHECK (recipient_kind IN ('members', 'organizer'));

ALTER TABLE event_notification_outbox
    DROP COLUMN IF EXISTS recipient_email;
