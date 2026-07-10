-- +goose Up
CREATE TABLE events (
    id UUID PRIMARY KEY,
    profile_id UUID REFERENCES profiles(id),
    title VARCHAR(255) NOT NULL,
    description TEXT,
    location VARCHAR(255),
    event_date TIMESTAMPTZ NOT NULL,
	capacity INTEGER,
	external_url VARCHAR(255),
	cancelled_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose Down
-- (ロールバック時はeventsを削除する)
DROP TABLE events;
