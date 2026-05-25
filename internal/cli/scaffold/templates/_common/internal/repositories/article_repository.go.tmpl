package repositories

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Article struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Published bool      `json:"published"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ListArticleParams struct {
	Query string
}

type CreateArticleParams struct {
	Title     string
	Content   string
	Published bool
}

type ArticleRepository struct {
	db *sql.DB
}

func NewArticleRepository(db *sql.DB) *ArticleRepository {
	return &ArticleRepository{db: db}
}

func (r *ArticleRepository) List(ctx context.Context, params ListArticleParams) ([]Article, error) {
	query := `SELECT id, title, content, published, created_at, updated_at FROM articles`
	args := make([]any, 0, 2)
	if search := strings.TrimSpace(params.Query); search != "" {
		like := "%" + search + "%"
		query += ` WHERE title LIKE ? OR content LIKE ?`
		args = append(args, like, like)
	}
	query += ` ORDER BY id DESC LIMIT 100`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Article, 0, 16)
	for rows.Next() {
		var it Article
		if err := rows.Scan(&it.ID, &it.Title, &it.Content, &it.Published, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ArticleRepository) Create(ctx context.Context, params CreateArticleParams) (Article, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(
		ctx,
		`INSERT INTO articles (created_at, updated_at, title, content, published) VALUES (?, ?, ?, ?, ?)`,
		now, now, params.Title, params.Content, params.Published,
	)
	if err != nil {
		return Article{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Article{}, err
	}

	return Article{
		ID:        id,
		Title:     params.Title,
		Content:   params.Content,
		Published: params.Published,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
