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

// minSecretLength rejects secrets too short to resist offline brute force
// of HS256 signatures.
const minSecretLength = 32

// placeholderSecrets are template defaults that must never reach production;
// accepting one would let anyone who has seen the template forge tokens.
var placeholderSecrets = map[string]struct{}{
	"your-access-token-secret":  {},
	"your-refresh-token-secret": {},
	"changeme":                  {},
	"secret":                    {},
}

func NewTokenService() (*TokenService, error) {
	secret := normalizeEnvValue(os.Getenv("JWT_ACCESS_TOKEN_SECRET"))
	if _, isPlaceholder := placeholderSecrets[secret]; secret == "" || isPlaceholder {
		return nil, errors.New("JWT_ACCESS_TOKEN_SECRET must be set to the secret shared with the auth service (placeholder or empty value rejected)")
	}
	if len(secret) < minSecretLength {
		return nil, fmt.Errorf("JWT_ACCESS_TOKEN_SECRET must be at least %d characters, got %d", minSecretLength, len(secret))
	}

	issuer := normalizeEnvValue(os.Getenv("JWT_ISSUER"))
	if issuer == "" || issuer == "your-app-name" {
		return nil, errors.New("JWT_ISSUER must be set to the issuer used by the auth service")
	}

	return &TokenService{
		secretKey: []byte(secret),
		issuer:    issuer,
	}, nil
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
