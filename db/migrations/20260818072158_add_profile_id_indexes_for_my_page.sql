-- +goose Up
-- マイページのイベント一覧 API（GET /api/v1/me/events）は3種別とも
-- 「自分の profile_id で引く」アクセスパターンになるため、その2経路を支える。

-- hosted（主催したイベント）の WHERE events.profile_id = $1 用。
CREATE INDEX events_profile_id_idx
    ON events (profile_id);

-- applied / attended（申し込み中・参加済み）の
-- JOIN event_members m ON m.event_id = e.id AND m.profile_id = $1 用。
-- 既存の UNIQUE(event_id, profile_id) は先頭列が event_id のため、
-- profile_id だけを条件にする今回の検索には使えない。
CREATE INDEX event_members_profile_id_idx
    ON event_members (profile_id);

-- +goose Down
DROP INDEX event_members_profile_id_idx;
DROP INDEX events_profile_id_idx;
