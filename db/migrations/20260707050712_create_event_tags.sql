-- +goose Up
-- event_tags はイベントとタグの中間テーブル。
CREATE TABLE event_tags (
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tag_id   UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE
    PRIMARY KEY (event_id, tag_id)
);

-- +goose Down
DROP TABLE event_tags;
