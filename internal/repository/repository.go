package repository

import (
	
	
	"context"

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
// WithTx runs fn inside a database transaction. If fn returns an error the
// transaction is rolled back; otherwise it commits. The *db.Queries passed to
// fn is bound to the transaction, so every query inside runs atomically.
func (r *Repository) WithTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op if the tx already committed

	if err := fn(db.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}