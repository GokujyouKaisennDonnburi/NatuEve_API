-- +goose Up

-- 参加状態ログ1件あたりのカテゴリ別人数。カテゴリは event_costs を参照する。
-- 内訳を持つのは action='join' のログのみで、leave ログには行を作らない。
-- 匿名参加は event_participation_logs に記録されないため、内訳も残らない。
CREATE TABLE event_participation_log_categories (
    participation_log_id UUID NOT NULL,
    cost_id              UUID NOT NULL,
    -- ログとカテゴリの event_id 一致を複合 FK で縛るための冗長列。
    event_id             UUID NOT NULL,
    head_count           INT NOT NULL CHECK (head_count >= 1),
    PRIMARY KEY (participation_log_id, cost_id),
    FOREIGN KEY (participation_log_id, event_id)
        REFERENCES event_participation_logs (id, event_id) ON DELETE CASCADE,
    FOREIGN KEY (cost_id, event_id)
        REFERENCES event_costs (id, event_id)
);

-- cost_id 単独での集計・FK チェック用（PK の先頭列ではないため別途張る）。
CREATE INDEX idx_event_participation_log_categories_cost
    ON event_participation_log_categories (cost_id);

-- +goose Down
DROP TABLE event_participation_log_categories;
