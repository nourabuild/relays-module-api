package jwt

import (
	"context"
	"errors"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "test-secret-0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	testIssuer = "relays-test"
)

func TestParseAccessTokenAcceptsValidToken(t *testing.T) {
	t.Parallel()

	service := NewTokenService(testSecret, testIssuer)

	claims, err := service.ParseAccessToken(context.Background(), signAccessToken(t, testSecret, "27"))
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if claims.Subject != "27" {
		t.Fatalf("expected subject 27, got %q", claims.Subject)
	}
}

func TestParseAccessTokenRejectsWrongSecret(t *testing.T) {
	t.Parallel()

	service := NewTokenService(testSecret, testIssuer)

	_, err := service.ParseAccessToken(context.Background(),
		signAccessToken(t, "wrong-secret-0f1e2d3c4b5a69788796a5b4", "27"))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseAccessTokenRejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	service := NewTokenService(testSecret, "other-issuer")

	_, err := service.ParseAccessToken(context.Background(), signAccessToken(t, testSecret, "27"))
	if !errors.Is(err, ErrInvalidClaims) {
		t.Fatalf("expected ErrInvalidClaims, got %v", err)
	}
}

func TestParseAccessTokenRejectsEmptyToken(t *testing.T) {
	t.Parallel()

	service := NewTokenService(testSecret, testIssuer)

	if _, err := service.ParseAccessToken(context.Background(), ""); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func signAccessToken(t *testing.T, secret, subject string) string {
	t.Helper()

	now := time.Now()
	claims := Claims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    testIssuer,
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(now.Add(15 * time.Minute)),
			NotBefore: gojwt.NewNumericDate(now),
		},
	}

	tokenString, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenString
}
