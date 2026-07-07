-- +goose Up
-- +goose StatementBegin
-- tags はイベントに付与するタグ。
CREATE TABLE tags (
    id           UUID PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE tags;
-- +goose StatementEnd