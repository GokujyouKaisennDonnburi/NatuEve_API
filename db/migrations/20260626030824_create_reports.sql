-- +goose Up
CREATE TABLE reports(
    id UUID PRIMARY KEY,
    event_id UUID REFERENCES events(id),
    content TEXT NOT NULL
);

-- +goose Down
DROP TABLE reports;