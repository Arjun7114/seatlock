package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Arjun7114/seatlock/internal/auth"
)

type contextKey string

const userIDKey contextKey = "userID"

// requireAuth verifies the Bearer token and puts the user ID in the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or malformed authorization header")
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		userID, err := auth.ParseToken(tokenStr, s.secret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
// rateLimit limits each client IP to `limit` requests per `window`.
// Over the limit → 429 Too Many Requests.
func (s *Server) rateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.cache == nil {
				next.ServeHTTP(w, r) // no cache configured → no limiting
				return
			}

			ip := r.RemoteAddr
			if host, _, err := net.SplitHostPort(ip); err == nil {
				ip = host
			}
			key := fmt.Sprintf("ratelimit:%s", ip)

			allowed, _ := s.cache.Allow(r.Context(), key, limit, window)
			if !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", window.Seconds()))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userIDFromContext retrieves the authenticated user ID set by requireAuth.
func userIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}