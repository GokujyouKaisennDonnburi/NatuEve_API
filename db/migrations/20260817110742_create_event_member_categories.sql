-- +goose Up

-- 1申込あたりのカテゴリ別人数（例: 大人2 / 学生1）。
-- 参加者カテゴリの実体は event_costs（イベント作成時に主催者が自由入力で定義する費用カテゴリ）で、
-- 参加申込は自前でカテゴリ名を打たず、そのイベントの費用カテゴリを参照して内訳を送る。
--
-- event_members.party_size には合計をサーバーが書き込む（定員チェックは SUM(party_size) を
-- イベント行ロック下で行う hot path のため、合計を非正規化で保持する）。
-- クライアントが送ってきた合計人数は信用せず、内訳から算出した値を使う。
--
-- head_count は 1 以上のみ許可し、0 名（例: 幼児0名）は行を作らない。
-- そのイベントで選べるカテゴリ一覧は event_costs から引けるため、0 行は「行が無い」のと
-- 同じ情報しか持たない。カテゴリ別集計に人数0の行が混ざらない利点もある。
CREATE TABLE event_member_categories (
    member_id  UUID NOT NULL,
    cost_id    UUID NOT NULL,
    -- 参加者とカテゴリが同一イベントに属することを複合 FK で縛るための冗長列。
    event_id   UUID NOT NULL,
    head_count INT NOT NULL CHECK (head_count >= 1),
    -- 同一申込内で同じカテゴリを2行に分けることを禁止する（人数は1行に集約させる）。
    PRIMARY KEY (member_id, cost_id),
    -- 参加取消（leave）で event_members の行が DELETE されるため CASCADE で追随させる。
    FOREIGN KEY (member_id, event_id)
        REFERENCES event_members (id, event_id) ON DELETE CASCADE,
    -- 別イベントの費用カテゴリを指定した申込を構造的に不可能にする。
    -- アプリ層の事前チェックはレース条件ですり抜けるため、この制約が信頼の拠り所。
    -- また NO ACTION（既定）のため、参加者がいるカテゴリの削除は DB が拒否する。
    -- 申込後に参加費の前提を書き換えられることへの防波堤（改名の禁止は API 層で行う）。
    FOREIGN KEY (cost_id, event_id)
        REFERENCES event_costs (id, event_id)
);

-- カテゴリ別の人数集計と、カテゴリ削除時の FK チェックのためのインデックス。
-- PK は (member_id, cost_id) の順なので cost_id 単独の検索には効かない。
CREATE INDEX idx_event_member_categories_cost
    ON event_member_categories (cost_id);

-- +goose Down
DROP TABLE event_member_categories;
