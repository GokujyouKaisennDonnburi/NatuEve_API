-- +goose Up
-- event_tags はイベントとタグの中間テーブル。
CREATE TABLE event_tags (
    event_id UUID REFERENCES events(id),
    tag_id   UUID REFERENCES tags(id),
    PRIMARY KEY (event_id, tag_id)
);

-- +goose Down
DROP TABLE event_tags;
