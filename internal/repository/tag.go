package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// TagRepository はタグ取得用Repositoryのインターフェース。
type TagRepository interface {
	// List はタグ一覧を取得する。
	List(ctx context.Context) ([]model.Tag, error)
}

// tagPostgres は TagRepository の PostgreSQL 実装。
type tagPostgres struct {
	db *sql.DB
}

// NewTagRepository はRepositoryを生成する。
func NewTagRepository(db *sql.DB) TagRepository {
	return &tagPostgres{db: db}
}

const listTagsQuery = `
SELECT
	id,
	name
FROM tags
ORDER BY name ASC
`

func (r *tagPostgres) List(
	ctx context.Context,
) ([]model.Tag, error) {
	rows, err := r.db.QueryContext(ctx, listTagsQuery)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	tags := make([]model.Tag, 0)

	for rows.Next() {
		var tag model.Tag

		if err := rows.Scan(
			&tag.ID,
			&tag.Name,
		); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}

		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}

	return tags, nil
}
