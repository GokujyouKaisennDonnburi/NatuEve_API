package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// ErrProfileNotFound はプロフィールが存在しないことを表すセンチネルエラー。
var ErrProfileNotFound = errors.New("profile not found")

// ProfileRepository は profiles テーブルへのアクセスを抽象化する。
type ProfileRepository interface {
	// GetByID は ID でプロフィールを取得する。無ければ ErrProfileNotFound。
	GetByID(ctx context.Context, id string) (*model.Profile, error)
	// Upsert はプロフィールを作成し、既存なら最新の値で更新する。
	// 実行後、p には DB 上の最新の状態(作成/更新日時を含む)が反映される。
	Upsert(ctx context.Context, p *model.Profile) error
}

// profilePostgres は ProfileRepository の PostgreSQL 実装。
type profilePostgres struct {
	db *sql.DB
}

// NewProfileRepository は *sql.DB を使う ProfileRepository を生成する。
func NewProfileRepository(db *sql.DB) ProfileRepository {
	return &profilePostgres{db: db}
}

// GetByID は ID でプロフィールを取得する。
func (r *profilePostgres) GetByID(ctx context.Context, id string) (*model.Profile, error) {
	const query = `
		SELECT id, email, display_name, avatar_url, description, created_at, updated_at
		FROM profiles
		WHERE id = $1`

	var (
		p           model.Profile
		displayName sql.NullString
		avatarURL   sql.NullString
		description sql.NullString
	)
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID, &p.Email, &displayName, &avatarURL, &description, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	p.DisplayName = displayName.String
	p.AvatarURL = avatarURL.String
	p.Description = description.String
	return &p, nil
}

// Upsert はプロフィールを作成または更新する。
func (r *profilePostgres) Upsert(ctx context.Context, p *model.Profile) error {
	const query = `
		INSERT INTO profiles (id, email, display_name, avatar_url, description)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			email        = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
    		avatar_url   = EXCLUDED.avatar_url,
    		description  = EXCLUDED.description,
			updated_at   = now()
		RETURNING id, email, display_name, description, created_at, updated_at`

	// avatar_url は Supabase 側で設定される値を使うため、この upsert では読み戻さない
	// （p.AvatarURL は呼び出し元が渡した JWT 由来の値を保持する）。ユーザーによる編集は
	// 別途新設予定の編集用エンドポイントで扱う。そのため RETURNING / Scan からは avatar_url を除く。
	var (
		displayName sql.NullString
		description sql.NullString
	)
	// description は NOT NULL DEFAULT '' のため、空文字を NULL 化せずそのまま渡す。
	// nullString で NULL 化すると NOT NULL 制約違反になる（DEFAULT は値未指定時のみ適用）。
	err := r.db.QueryRowContext(ctx, query,
		p.ID, p.Email, nullString(p.DisplayName), nullString(p.AvatarURL), p.Description,
	).Scan(&p.ID, &p.Email, &displayName, &description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert profile: %w", err)
	}
	p.DisplayName = displayName.String
	p.Description = description.String
	return nil
}

// nullString は空文字を NULL として扱うための変換を行う。
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
