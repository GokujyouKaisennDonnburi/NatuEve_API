-- +goose Up
-- ゲストの参加申込を確認メールで確定するための状態とトークン（ADR-0044）。
-- pending は確認待ちで、参加者一覧・人数・定員判定には含めない。
-- トークンは平文を保存せず、SHA-256 の hex だけを保存する。
ALTER TABLE event_members
    ADD COLUMN status TEXT NOT NULL DEFAULT 'confirmed'
        CHECK (status IN ('pending', 'confirmed')),
    ADD COLUMN confirmation_token_hash TEXT,
    ADD COLUMN confirmation_token_expires_at TIMESTAMPTZ;

-- 既存行の backfill が済んだら既定値を外す。status を指定し忘れた INSERT が
-- 確定扱いになる事故を防ぐ（ADR-0044）。
ALTER TABLE event_members
    ALTER COLUMN status DROP DEFAULT;

ALTER TABLE event_members
    ADD CONSTRAINT event_members_confirmation_token_check
    CHECK (
        (status = 'pending'
            AND confirmation_token_hash IS NOT NULL
            AND confirmation_token_expires_at IS NOT NULL)
        OR
        (status = 'confirmed'
            AND confirmation_token_hash IS NULL
            AND confirmation_token_expires_at IS NULL)
    );

-- トークンの検索を高速化する。使用済みの行は NULL のため部分インデックスに含めない。
CREATE UNIQUE INDEX event_members_confirmation_token_hash_key
    ON event_members (confirmation_token_hash)
    WHERE confirmation_token_hash IS NOT NULL;

-- 期限切れ pending の遅延削除を高速化する。
CREATE INDEX idx_event_members_pending_expires
    ON event_members (event_id, confirmation_token_expires_at)
    WHERE status = 'pending';

-- +goose Down
-- 未確定の申込を確定扱いで残さないため、status を落とす前に削除する。
-- 確認メールの予約も残すと、削除済みの申込へのリンクが送られるため合わせて消す。
DELETE FROM event_notification_outbox
WHERE recipient_kind = 'email'
  AND (event_id, lower(recipient_email)) IN (
      SELECT event_id, lower(mail_address)
      FROM event_members
      WHERE status = 'pending'
  );

DELETE FROM event_members
WHERE status = 'pending';

ALTER TABLE event_members
    DROP CONSTRAINT IF EXISTS event_members_confirmation_token_check;

DROP INDEX IF EXISTS idx_event_members_pending_expires;
DROP INDEX IF EXISTS event_members_confirmation_token_hash_key;

ALTER TABLE event_members
    DROP COLUMN IF EXISTS confirmation_token_expires_at,
    DROP COLUMN IF EXISTS confirmation_token_hash,
    DROP COLUMN IF EXISTS status;
