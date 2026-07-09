-- +goose Up
-- tags はイベントに付与するタグ。
CREATE TABLE tags (
    id          UUID PRIMARY KEY,
    name    VARCHAR(255) NOT NULL UNIQUE,
    normalized_name VARCHAR(30) NOT NULL UNIQUE
);

-- +goose Down
DROP TABLE tags;