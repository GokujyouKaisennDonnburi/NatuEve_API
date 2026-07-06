-- +goose Up
CREATE TABLE event_members (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id),
    profile_id UUID REFERENCES profiles(id),
    username TEXT NOT NULL,
    mail_address TEXT NOT NULL,
    -- 代表者を含む参加人数。団体登録（代表者＋同伴者氏名）導入時は 1 + 同伴者数 をセットする。
    party_size INT NOT NULL DEFAULT 1 CHECK (party_size >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (event_id, profile_id)
);

-- 同一イベントへの同一メールアドレスの重複申込を DB レベルで禁止する。
-- アプリ側の事前チェックはレース条件ですり抜けるため、この制約が信頼の拠り所。
-- 大文字小文字の揺れ（Foo@example.com / foo@example.com）をすり抜けさせないよう lower() で比較する。
-- ※ RFC 上ローカルパートは大小区別があり得るが、実プロバイダはほぼ全て無視するため同一視する
--   （区別すると大小の切り替えだけで重複チェックを素通りできてしまう）。
CREATE UNIQUE INDEX event_members_event_id_lower_mail_key
    ON event_members (event_id, lower(mail_address));
-- +goose Down
DROP TABLE event_members;
