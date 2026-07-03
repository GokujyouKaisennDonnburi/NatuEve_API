-- +goose Up
-- 参加申込に人数（代表者含む）を持たせる。既存行は1名扱い。
-- 将来の団体登録（代表者＋同伴者氏名）では 1 + 同伴者数 をセットする。
ALTER TABLE event_members
    ADD COLUMN party_size INT NOT NULL DEFAULT 1 CHECK (party_size >= 1);

-- 同一イベントへの同一メールアドレスの重複申込を DB レベルで禁止する。
-- アプリ側の事前チェックはレース条件ですり抜けるため、この制約が信頼の拠り所。
-- 大文字小文字の揺れ（Foo@example.com / foo@example.com）をすり抜けさせないよう lower() で比較する。
CREATE UNIQUE INDEX event_members_event_id_lower_mail_key
    ON event_members (event_id, lower(mail_address));

-- +goose Down
DROP INDEX event_members_event_id_lower_mail_key;
ALTER TABLE event_members
    DROP COLUMN party_size;
