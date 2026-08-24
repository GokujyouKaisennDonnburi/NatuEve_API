-- +goose Up
CREATE TABLE events (
    id UUID PRIMARY KEY,
    profile_id UUID REFERENCES profiles(id),
    title VARCHAR(255) NOT NULL,
    description TEXT,
    location VARCHAR(255),
    event_date TIMESTAMPTZ NOT NULL,
    end_date TIMESTAMPTZ NOT NULL,
    -- NULL は申込期限なし（ADR-0029）
    application_deadline TIMESTAMPTZ,
	capacity INTEGER,
	external_url VARCHAR(255),
	cancelled_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT events_end_date_after_event_date CHECK (end_date >= event_date),
	CONSTRAINT events_application_deadline_before_end_date CHECK (application_deadline <= end_date)
);
-- +goose Down
-- (ロールバック時はeventsを削除する)
DROP TABLE events;
