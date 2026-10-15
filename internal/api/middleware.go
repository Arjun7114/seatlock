package api

import (
	"context"
	"net/http"
	"strings"

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

// userIDFromContext retrieves the authenticated user ID set by requireAuth.
func userIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}