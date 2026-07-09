package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ErrDuplicateTag はタグ名の重複を示すエラー。
var ErrDuplicateTag = errors.New("duplicate tag")

// TagRepository はタグ取得用Repositoryのインターフェース。
type TagRepository interface {
	// List はタグ一覧を取得する。
	List(ctx context.Context) ([]model.Tag, error)
	// Create はタグを作成する。
	Create(ctx context.Context, name string, normalizedName string) (model.Tag, error)
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

const createTagQuery = `
INSERT INTO tags (
	name,
	normalized_name
)
VALUES ($1, $2)
RETURNING
	id,
	name
`

func (r *tagPostgres) Create(
	ctx context.Context,
	name string,
	normalizedName string,
) (model.Tag, error) {

	var tag model.Tag

	err := r.db.QueryRowContext(
		ctx,
		createTagQuery,
		name,
		normalizedName,
	).Scan(
		&tag.ID,
		&tag.Name,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			(pgErr.ConstraintName == "tags_name_key" ||
				pgErr.ConstraintName == "tags_normalized_name_key") {
			return model.Tag{}, ErrDuplicateTag
		}
		return model.Tag{}, fmt.Errorf(
			"create tag: %w",
			err,
		)
	}

	return tag, nil
}
