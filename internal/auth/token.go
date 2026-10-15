package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL is how long an issued token stays valid.
const TokenTTL = 24 * time.Hour

var ErrInvalidToken = errors.New("invalid or expired token")

// Claims is the JWT payload. Subject carries the user ID.
type Claims struct {
	UserID int64 `json:"uid"`
	jwt.RegisteredClaims
}

// IssueToken creates a signed JWT for the given user.
func IssueToken(userID int64, secret string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
			Issuer:    "seatlock",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken verifies a token's signature and expiry, returning the user ID.
func ParseToken(tokenString, secret string) (int64, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Reject anything not signed with HMAC — prevents algorithm-confusion attacks.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return 0, ErrInvalidToken
	}
	return claims.UserID, nil
}