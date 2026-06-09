// Package jwt provides token validation for external JWT tokens.
package jwt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken     = errors.New("invalid_token")
	ErrExpiredToken     = errors.New("expired_token")
	ErrTokenNotFound    = errors.New("token_not_found")
	ErrInvalidClaims    = errors.New("invalid_claims")
	ErrTokenNotYetValid = errors.New("token_not_yet_valid")
)

// Claims represents the JWT claims from external tokens
type Claims struct {
	IsAdmin bool `json:"is_admin"`
	jwt.RegisteredClaims
}

type TokenService struct {
	secretKey []byte
	issuer    string
}

func NewTokenService() *TokenService {
	issuer := envOrDefault("JWT_ISSUER", "your-app-name")
	secret := envOrDefault("JWT_ACCESS_TOKEN_SECRET", "your-access-token-secret")

	return &TokenService{
		secretKey: []byte(secret),
		issuer:    issuer,
	}
}

func envOrDefault(key, fallback string) string {
	if value := normalizeEnvValue(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func normalizeEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}

	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}

	return value
}

func (s *TokenService) ParseAccessToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Check if token exists
	if tokenString == "" {
		return nil, ErrTokenNotFound
	}

	// Parse and validate token
	claims := &Claims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithStrictDecoding(),
		jwt.WithIssuer(s.issuer),
	)

	token, err := parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Verify signing method is HMAC
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secretKey, nil
	})

	// Handle parsing errors
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, ErrExpiredToken
		case errors.Is(err, jwt.ErrTokenNotValidYet):
			return nil, ErrTokenNotYetValid
		case errors.Is(err, jwt.ErrTokenMalformed):
			return nil, ErrInvalidToken
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return nil, ErrInvalidToken
		case errors.Is(err, jwt.ErrTokenInvalidClaims):
			return nil, ErrInvalidClaims
		default:
			return nil, ErrInvalidToken
		}
	}

	// Final validity check
	if !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
