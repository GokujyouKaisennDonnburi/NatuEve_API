-- +goose Up
CREATE TABLE event_join_status (
    event_id UUID NOT NULL,
    join_status VARCHAR(10) NOT NULL,
    deleted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE event_join_status;
