package config

import "os"

// DatabaseURL returns the Postgres connection string, overridable by env var.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://seatlock:seatlock@localhost:5432/seatlock?sslmode=disable"
}