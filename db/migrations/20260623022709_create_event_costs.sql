-- +goose Up
CREATE TABLE event_costs (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    category VARCHAR(255) NOT NULL,
    cost INTEGER NOT NULL,
    -- 内訳テーブルからの複合 FK の参照先。
    UNIQUE (id, event_id)
);

-- 同一イベント内のカテゴリ名の重複を禁止する（大文字小文字は区別しない）。
CREATE UNIQUE INDEX event_costs_event_id_lower_category_key
    ON event_costs (event_id, lower(category));

-- +goose Down
-- (ロールバック時は event_costs を削除する)
DROP TABLE event_costs;
