-- +goose Up
CREATE TABLE event_members (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL   REFERENCES events(id),
    profile_id UUID NOT NULL REFERENCES profiles(id),
    username TEXT NOT NULL,         
    mail_address TEXT NOT NULL
);
-- +goose Down
DROP TABLE event_members;
