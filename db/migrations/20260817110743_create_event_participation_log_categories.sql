-- +goose Up

-- 参加状態ログのカテゴリ別人数の内訳。action='join' 時点のスナップショットを残す。
-- ログ自体が追記専用のため、この内訳も後から更新しない。
-- action='leave' は全キャンセルのみを扱い部分キャンセルがないため、leave ログには内訳行を作らない。
--
-- 匿名参加（profile_id NULL）は event_participation_logs 自体に記録されないため、
-- 匿名参加の内訳は event_member_categories 側にのみ残る。
-- カテゴリの実体・head_count の方針は event_member_categories と揃える。
CREATE TABLE event_participation_log_categories (
    participation_log_id UUID NOT NULL,
    cost_id              UUID NOT NULL,
    -- ログとカテゴリが同一イベントに属することを複合 FK で縛るための冗長列。
    event_id             UUID NOT NULL,
    head_count           INT NOT NULL CHECK (head_count >= 1),
    -- 同一ログ内で同じカテゴリを2行に分けることを禁止する。
    PRIMARY KEY (participation_log_id, cost_id),
    -- ログは物理削除しない運用だが、将来の保持期間ポリシー導入時に孤児行が残らないよう CASCADE にする。
    FOREIGN KEY (participation_log_id, event_id)
        REFERENCES event_participation_logs (id, event_id) ON DELETE CASCADE,
    -- 別イベントの費用カテゴリを指定したログを構造的に不可能にする。
    -- NO ACTION（既定）のため、内訳が残っているカテゴリの削除は DB が拒否する。
    FOREIGN KEY (cost_id, event_id)
        REFERENCES event_costs (id, event_id)
);

-- カテゴリ別の人数集計と、カテゴリ削除時の FK チェックのためのインデックス。
-- PK は (participation_log_id, cost_id) の順なので cost_id 単独の検索には効かない。
CREATE INDEX idx_event_participation_log_categories_cost
    ON event_participation_log_categories (cost_id);

-- +goose Down
DROP TABLE event_participation_log_categories;
