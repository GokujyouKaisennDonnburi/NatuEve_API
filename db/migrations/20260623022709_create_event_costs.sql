-- +goose Up
CREATE TABLE event_costs (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    category VARCHAR(255) NOT NULL,
    cost INTEGER NOT NULL,
    -- 参加者カテゴリの実体はこの費用カテゴリ。内訳（event_member_categories）が
    -- カテゴリを一意に指せるよう、同一イベント内での重複を禁止する。
    -- 「大人 500円」「大人 800円」が併存すると内訳がどちらの金額を指すか決まらない。
    UNIQUE (event_id, category),
    -- 内訳テーブルからの複合 FK の参照先。id は PK なので一意性としては冗長だが、
    -- 複合 FK は参照先に同じ列組の UNIQUE 制約を要求するため明示的に張る。
    UNIQUE (id, event_id)
);

-- +goose Down
-- (ロールバック時は event_costs を削除する)
DROP TABLE event_costs;
