package api

import (
	"encoding/json"
	"log/slog"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Arjun7114/seatlock/internal/cache"
	"github.com/Arjun7114/seatlock/internal/config"
	"github.com/Arjun7114/seatlock/internal/repository"
	"github.com/Arjun7114/seatlock/internal/service"
)

type Server struct {
	repo     *repository.Repository
	authSvc  *service.AuthService
	booking  *service.BookingService
	strategy string
	logger   *slog.Logger
	secret   string
	cache    *cache.Cache
}

func NewServer(repo *repository.Repository, booking *service.BookingService, authSvc *service.AuthService, c *cache.Cache, secret, strategy string, logger *slog.Logger) *Server {
	return &Server{repo: repo, booking: booking, authSvc: authSvc, cache: c, secret: secret, strategy: strategy, logger: logger}
}

// Routes builds the chi router with middleware and endpoints.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	if config.TrustProxy() {
		r.Use(middleware.RealIP) // trust X-Forwarded-For only behind a known proxy
	}
	r.Use(middleware.Recoverer)
	if rpm := config.RateLimitPerMinute(); rpm > 0 {
		r.Use(s.rateLimit(rpm, time.Minute))
	}

	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		// Public reads
		r.Get("/events", s.handleListEvents)
		r.Get("/events/{eventID}/seats", s.handleListSeats)

		// Public auth
		r.Post("/auth/signup", s.handleSignup)
		r.Post("/auth/login", s.handleLogin)

		// Protected — requires a valid JWT
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/seats/{seatID}/hold", s.handlePlaceHold)
			r.Post("/seats/{seatID}/confirm", s.handleConfirm)
		})
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.repo.ListEvents(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleListSeats(w http.ResponseWriter, r *http.Request) {
	eventID, err := strconv.ParseInt(chi.URLParam(r, "eventID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event id")
		return
	}

	cacheKey := fmt.Sprintf("seats:event:%d", eventID)

	// 1. Try cache first.
	if s.cache != nil {
		if cached, err := s.cache.Get(r.Context(), cacheKey); err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(cached))
			return
		}
	}

	// 2. Cache miss — query the DB.
	seats, err := s.repo.ListSeatsForEvent(r.Context(), eventID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// 3. Marshal, store in cache (best-effort, short TTL), return.
	body, err := json.Marshal(seats)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if s.cache != nil {
		_ = s.cache.Set(r.Context(), cacheKey, string(body), 30*time.Second)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("internal error",
		"err", err,
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", middleware.GetReqID(r.Context()),
	)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
type bookingRequest struct {
	UserID int64 `json:"user_id"` // pre-auth: replaced by JWT identity in Phase 3
}

func (s *Server) handlePlaceHold(w http.ResponseWriter, r *http.Request) {
	seatID, err := strconv.ParseInt(chi.URLParam(r, "seatID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid seat id")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	holdID, err := s.booking.PlaceHold(r.Context(), seatID, userID)
	if err != nil {
		s.mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"hold_id": holdID, "seat_id": seatID})
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	seatID, err := strconv.ParseInt(chi.URLParam(r, "seatID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid seat id")
		return
	}
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	bookingID, err := s.booking.Confirm(r.Context(), s.strategy, seatID, userID)
	if err != nil {
		s.mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"booking_id": bookingID, "seat_id": seatID})
}

// mapServiceError translates domain errors into HTTP status codes.
func (s *Server) mapServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrSeatNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSeatBooked),
		errors.Is(err, service.ErrSeatHeld),
		errors.Is(err, service.ErrHoldOtherUser):
		writeError(w, http.StatusConflict, err.Error()) // 409
	case errors.Is(err, service.ErrNoHold):
		writeError(w, http.StatusUnprocessableEntity, err.Error()) // 422
	default:
		s.serverError(w, r, err)
	}
}