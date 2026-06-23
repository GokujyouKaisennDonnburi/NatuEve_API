-- +goose Up
CREATE TABLE event_images (
    id UUID PRIMARY KEY,
    event_id UUID REFERENCES events(id) ON DELETE CASCADE,
    image_objectkey TEXT NOT NULL
);

-- +goose Down
-- (ロールバック時は event_images を削除する)
DROP TABLE event_images;