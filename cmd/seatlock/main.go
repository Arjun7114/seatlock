package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Arjun7114/seatlock/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.DatabaseURL())
	if err != nil {
		logger.Error("failed to create connection pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Verify the connection is actually alive.
	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "err", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Read seeded data back out as a smoke test.
	var events, seats int
	err = pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM events), (SELECT count(*) FROM seats)`,
	).Scan(&events, &seats)
	if err != nil {
		logger.Error("query failed", "err", err)
		os.Exit(1)
	}

	fmt.Printf("seatlock: db OK — %d event(s), %d seat(s)\n", events, seats)
}