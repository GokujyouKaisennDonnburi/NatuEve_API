-- +goose Up
CREATE TABLE reports(
    id UUID PRIMARY KEY,
    event_id UUID REFERENCES events(id),
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE reports;