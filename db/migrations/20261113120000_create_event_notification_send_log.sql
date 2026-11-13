-- +goose Up
-- event_notification_send_log は一斉送信（POST /events/{id}/notifications）の
-- 送信回数上限のカウント用テーブル（ADR-0046）。1 回の送信につき 1 行を送信前に
-- 予約（INSERT）し、直近 24 時間分の行数で回数を数える。送信成功時は sent_at を
-- 記録し、送信失敗時は行を削除して枠を返す。
-- 複数インスタンスからの同時予約はリポジトリが pg_advisory_xact_lock で
-- イベント単位に直列化するため、行数の数え漏れが起きない。
CREATE TABLE event_notification_send_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id    UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    reserved_at TIMESTAMPTZ NOT NULL,
    sent_at     TIMESTAMPTZ
);

-- 期間内の行数カウント（event_id と reserved_at で絞り込む）を高速化するインデックス。
CREATE INDEX idx_event_notification_send_log_event_reserved
    ON event_notification_send_log (event_id, reserved_at);

-- +goose Down
DROP TABLE event_notification_send_log;
