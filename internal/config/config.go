package config

import (
	"os"
	"strconv"
	"strings"
)

// DatabaseURL returns the Postgres connection string, overridable by env var.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://seatlock:seatlock@localhost:5432/seatlock?sslmode=disable"
}

// LockStrategy returns "pessimistic" (default) or "optimistic", from LOCK_STRATEGY.
func LockStrategy() string {
	if v := os.Getenv("LOCK_STRATEGY"); v == "optimistic" {
		return "optimistic"
	}
	return "pessimistic"
}

// JWTSecret returns the signing secret for JWTs.
func JWTSecret() string {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return v
	}
	// Dev-only fallback. In production this MUST come from the environment.
	return "dev-secret-change-me"
}

// RedisAddr returns the Redis address, overridable by env var.
func RedisAddr() string {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		return v
	}
	return "localhost:6379"
}

// RateLimitPerMinute returns the per-client-IP request limit per minute, from
// RATE_LIMIT_PER_MINUTE. 0 disables rate limiting entirely. Default 100.
func RateLimitPerMinute() int {
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_PER_MINUTE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 100
}

// TrustProxy reports whether the X-Forwarded-For header may be trusted to
// determine the client IP, from TRUST_PROXY ("true" enables it). Default false.
// Enable ONLY when seatlock runs behind a proxy/load balancer that overwrites
// this header; otherwise clients can spoof it to evade per-IP rate limiting.
func TrustProxy() bool {
	return strings.TrimSpace(os.Getenv("TRUST_PROXY")) == "true"
}