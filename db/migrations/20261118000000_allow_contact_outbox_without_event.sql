-- +goose Up
-- お問い合わせはイベントに紐づかないemail宛通知として保存する（ADR-0052）。
ALTER TABLE event_notification_outbox ALTER COLUMN event_id DROP NOT NULL;
ALTER TABLE event_notification_outbox
    ADD CONSTRAINT event_notification_outbox_event_required_check
    CHECK (event_id IS NOT NULL OR recipient_kind = 'email');

-- +goose Down
-- 戻す前にイベントなしの送信待ち行が残っていないか確認する（ADR-0052）。
DELETE FROM event_notification_outbox WHERE event_id IS NULL;
ALTER TABLE event_notification_outbox
    DROP CONSTRAINT event_notification_outbox_event_required_check;
ALTER TABLE event_notification_outbox ALTER COLUMN event_id SET NOT NULL;
