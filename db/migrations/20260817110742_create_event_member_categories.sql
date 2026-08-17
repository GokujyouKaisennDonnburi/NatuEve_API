-- +goose Up

-- 1申込あたりのカテゴリ別人数（例: 大人2 / 学生1）。カテゴリは event_costs を参照する。
-- 合計人数は event_members.party_size に保持する。
CREATE TABLE event_member_categories (
    member_id  UUID NOT NULL,
    cost_id    UUID NOT NULL,
    -- 参加者とカテゴリの event_id 一致を複合 FK で縛るための冗長列。
    event_id   UUID NOT NULL,
    head_count INT NOT NULL CHECK (head_count >= 1),
    PRIMARY KEY (member_id, cost_id),
    FOREIGN KEY (member_id, event_id)
        REFERENCES event_members (id, event_id) ON DELETE CASCADE,
    FOREIGN KEY (cost_id, event_id)
        REFERENCES event_costs (id, event_id)
);

-- cost_id 単独での集計・FK チェック用（PK の先頭列ではないため別途張る）。
CREATE INDEX idx_event_member_categories_cost
    ON event_member_categories (cost_id);

-- +goose Down
DROP TABLE event_member_categories;
