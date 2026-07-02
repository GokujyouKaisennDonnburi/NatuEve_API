-- +goose Up
CREATE TABLE event_members (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL   REFERENCES events(id),
    profile_id UUID REFERENCES profiles(id),
    username TEXT NOT NULL,
    mail_address TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (event_id, profile_id)
);

-- +goose Down
DROP TABLE event_members;
