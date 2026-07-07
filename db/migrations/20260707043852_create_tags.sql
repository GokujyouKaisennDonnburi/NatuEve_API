-- +goose Up
-- tags はイベントに付与するタグ。
CREATE TABLE tags (
    id          UUID PRIMARY KEY,
    tag_name    VARCHAR(255) NOT NULL UNIQUE
);

-- +goose Down
DROP TABLE tags;