-- +goose Up
-- イベント一覧の地域絞り込み（GET /api/v1/events?location=）が発行する
-- normalize(location, NFKC) ILIKE '%...%' を支えるインデックス（ADR-0030）。
-- 中間一致は B-tree の性質上インデックスが効かない。pg_trgm の GIN インデックスを使用する。
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- 式は internal/repository/event.go の buildSearchWhere が生成する条件式と
-- 完全に一致している必要がある。片方だけ変更するとインデックスは使われなくなり、
-- エラーではなく性能劣化として現れる。
-- normalize() は IMMUTABLE のため式インデックスに使用できる。
CREATE INDEX events_location_trgm_idx
    ON events USING gin (normalize(location, NFKC) gin_trgm_ops);

-- +goose Down
-- pg_trgm 拡張は他の用途から参照されうるため削除しない（ADR-0030）。
DROP INDEX events_location_trgm_idx;
