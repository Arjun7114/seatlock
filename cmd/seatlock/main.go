package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Arjun7114/seatlock/internal/api"
	"github.com/Arjun7114/seatlock/internal/config"
	"github.com/Arjun7114/seatlock/internal/repository"
	"github.com/Arjun7114/seatlock/internal/service"
	"github.com/Arjun7114/seatlock/internal/cache"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(ctx, config.DatabaseURL())
	cancel()
	if err != nil {
		logger.Error("failed to create connection pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
		redisCache, err := cache.New(context.Background(), config.RedisAddr())
	if err != nil {
		logger.Error("failed to connect to redis", "err", err)
		os.Exit(1)
	}
	defer redisCache.Close()
	logger.Info("connected to redis")

	repo := repository.New(pool)
	bookingSvc := service.NewBookingService(repo, redisCache)
	strategy := config.LockStrategy()
	logger.Info("lock strategy selected", "strategy", strategy)
	secret := config.JWTSecret()
	authSvc := service.NewAuthService(repo, secret)
	srv := api.NewServer(repo, bookingSvc, authSvc, redisCache, secret, strategy, logger)
	
	
	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      srv.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	logger.Info("starting http server", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}