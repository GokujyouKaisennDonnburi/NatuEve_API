-- +goose Up
-- 参加者一覧 API（GET /api/v1/events/{id}/members）の ORDER BY created_at を
-- 支え、参加者が多いイベントでもソートコストが線形に増えないようにする。
--
-- 既存の UNIQUE(event_id, profile_id) は search 用途には使えるが、
-- (event_id, created_at) 順のソートは支えられない。ソート +
-- WHERE event_id = $1 の組み合わせを直接カバーする複合インデックスを追加する。
-- ListRecipients（mail_address のみ SELECT）も同じアクセスパターンで効く。
CREATE INDEX event_members_event_id_created_at_idx
    ON event_members (event_id, created_at);

-- +goose Down
DROP INDEX event_members_event_id_created_at_idx;
