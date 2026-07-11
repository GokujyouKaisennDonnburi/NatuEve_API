-- +goose Up
-- event_notification_outbox は Transactional Outbox パターンによる通知予約テーブル。
-- イベントキャンセル確定と同一トランザクションで INSERT することで、
-- キャンセル確定と通知予約の原子性を保証する。宛先はスナップショットせず、
-- ワーカーが送信直前に event_members から解決する。
CREATE TABLE event_notification_outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        UUID NOT NULL REFERENCES events(id),
    subject         VARCHAR(255) NOT NULL,
    body            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ワーカーが送信対象を取得する際のクエリ（status='pending' AND next_attempt_at <= now()）を
-- 高速化する部分インデックス。pending 以外の行（sent/failed）は対象外のため含めない。
CREATE INDEX idx_event_notification_outbox_pending_next_attempt
    ON event_notification_outbox (next_attempt_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE event_notification_outbox;
