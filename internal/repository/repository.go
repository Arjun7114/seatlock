package repository

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Arjun7114/seatlock/internal/db"
)

// Repository wraps the sqlc-generated queries over a pgx pool.
type Repository struct {
	pool *pgxpool.Pool
	*db.Queries
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:    pool,
		Queries: db.New(pool),
	}
}

// Pool exposes the underlying pool for transaction work (Phase 4).
func (r *Repository) Pool() *pgxpool.Pool {
	return r.pool
}