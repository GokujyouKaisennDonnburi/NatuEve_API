-- +goose Up
-- +goose StatementBegin
-- profiles はアプリ側のユーザープロフィール。
-- id は Supabase Auth が発行する JWT の sub(UUID) と一致させる(自前で採番しない)。
CREATE TABLE profiles (
    id           UUID PRIMARY KEY,
    email        TEXT NOT NULL,
    display_name TEXT,
    avatar_url   TEXT,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE profiles;
-- +goose StatementEnd
