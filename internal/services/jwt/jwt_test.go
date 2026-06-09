package jwt

import (
	"context"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

func TestNewTokenServiceNormalizesQuotedAccessSecret(t *testing.T) {
	const secret = "your-access-token-secret"
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", `"`+secret+`"`)
	t.Setenv("JWT_ISSUER", "your-app-name")

	tokenString := signAccessToken(t, secret, "27")
	claims, err := NewTokenService().ParseAccessToken(context.Background(), tokenString)
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if claims.Subject != "27" {
		t.Fatalf("expected subject 27, got %q", claims.Subject)
	}
}

func TestEnvOrDefaultNormalizesMatchingQuotes(t *testing.T) {
	t.Setenv("QUOTED_DOUBLE", `"value"`)
	t.Setenv("QUOTED_SINGLE", `'value'`)

	if got := envOrDefault("QUOTED_DOUBLE", "fallback"); got != "value" {
		t.Fatalf("double-quoted value = %q, want value", got)
	}
	if got := envOrDefault("QUOTED_SINGLE", "fallback"); got != "value" {
		t.Fatalf("single-quoted value = %q, want value", got)
	}
}

func signAccessToken(t *testing.T, secret, subject string) string {
	t.Helper()

	now := time.Now()
	claims := Claims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    "your-app-name",
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
