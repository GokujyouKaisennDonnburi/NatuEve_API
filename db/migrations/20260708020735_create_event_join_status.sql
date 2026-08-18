-- +goose Up
CREATE TABLE event_participation_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id),
    profile_id UUID NOT NULL REFERENCES profiles(id),
    action     TEXT NOT NULL CHECK (action IN ('join', 'leave')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- event_participation_log_categories からの複合 FK の参照先。
    UNIQUE (id, event_id)
);

CREATE INDEX idx_event_participation_logs_event_profile
    ON event_participation_logs (event_id, profile_id, created_at);
-- +goose Down
DROP TABLE event_participation_logs;