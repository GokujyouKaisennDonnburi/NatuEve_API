-- +goose Up
CREATE TABLE event_costs (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    category VARCHAR(255) NOT NULL,
    cost INTEGER NOT NULL,
    -- 内訳テーブルからの複合 FK の参照先。id は PK なので一意性としては冗長だが、
    -- 複合 FK は参照先に同じ列組の UNIQUE 制約を要求するため明示的に張る。
    UNIQUE (id, event_id)
);

-- 参加者カテゴリの実体はこの費用カテゴリ。内訳（event_member_categories）が
-- カテゴリを一意に指せるよう、同一イベント内での重複を禁止する。
-- 「大人 500円」「大人 800円」が併存すると内訳がどちらの金額を指すか決まらない。
-- 大文字小文字の揺れ（Adult / adult）だけで二重登録されるのを防ぐため lower() で比較する
-- （event_members の mail_address と同じ方針）。
CREATE UNIQUE INDEX event_costs_event_id_lower_category_key
    ON event_costs (event_id, lower(category));

-- +goose Down
-- (ロールバック時は event_costs を削除する)
DROP TABLE event_costs;
