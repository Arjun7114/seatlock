package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"errors"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Arjun7114/seatlock/internal/repository"
	"github.com/Arjun7114/seatlock/internal/service"
)

type Server struct {
	repo     *repository.Repository
	booking  *service.BookingService
	strategy string
	logger   *slog.Logger
}

func NewServer(repo *repository.Repository, booking *service.BookingService, strategy string, logger *slog.Logger) *Server {
	return &Server{repo: repo, booking: booking, strategy: strategy, logger: logger}
}

// Routes builds the chi router with middleware and endpoints.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/events", s.handleListEvents)
		r.Get("/events/{eventID}/seats", s.handleListSeats)
		r.Post("/seats/{seatID}/hold", s.handlePlaceHold)
		r.Post("/seats/{seatID}/confirm", s.handleConfirm)
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

	seats, err := s.repo.ListSeatsForEvent(r.Context(), eventID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, seats)
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
	var req bookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 {
		writeError(w, http.StatusBadRequest, "valid user_id required in body")
		return
	}

	holdID, err := s.booking.PlaceHold(r.Context(), seatID, req.UserID)
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
	var req bookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 {
		writeError(w, http.StatusBadRequest, "valid user_id required in body")
		return
	}

	bookingID, err := s.booking.Confirm(r.Context(), s.strategy, seatID, req.UserID)
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