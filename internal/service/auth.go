package service

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Arjun7114/seatlock/internal/auth"
	"github.com/Arjun7114/seatlock/internal/db"
	"github.com/Arjun7114/seatlock/internal/repository"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

type AuthService struct {
	repo   *repository.Repository
	secret string
}

func NewAuthService(repo *repository.Repository, secret string) *AuthService {
	return &AuthService{repo: repo, secret: secret}
}

// Signup creates a user and returns a signed token.
func (s *AuthService) Signup(ctx context.Context, email, password string) (string, int64, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", 0, err
	}

	user, err := s.repo.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
	})
	if err != nil {
		// Unique violation on email → friendly error.
		if strings.Contains(err.Error(), "users_email_key") {
			return "", 0, ErrEmailTaken
		}
		return "", 0, err
	}

	token, err := auth.IssueToken(user.ID, s.secret)
	if err != nil {
		return "", 0, err
	}
	return token, user.ID, nil
}

// Login verifies credentials and returns a signed token.
func (s *AuthService) Login(ctx context.Context, email, password string) (string, int64, error) {
	email = strings.TrimSpace(strings.ToLower(email))

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, ErrInvalidCredentials // don't reveal whether email exists
		}
		return "", 0, err
	}

	ok, err := auth.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		return "", 0, ErrInvalidCredentials
	}

	token, err := auth.IssueToken(user.ID, s.secret)
	if err != nil {
		return "", 0, err
	}
	return token, user.ID, nil
}