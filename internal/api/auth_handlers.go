package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Arjun7114/seatlock/internal/service"
)

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token  string `json:"token"`
	UserID int64  `json:"user_id"`
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email required and password must be at least 8 characters")
		return
	}

	token, userID, err := s.authSvc.Signup(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{Token: token, UserID: userID})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, userID, err := s.authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, authResponse{Token: token, UserID: userID})
}