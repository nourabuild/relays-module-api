package jwt

import (
	"context"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "test-secret-0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	testIssuer = "relays-test"
)

func TestNewTokenServiceNormalizesQuotedAccessSecret(t *testing.T) {
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", `"`+testSecret+`"`)
	t.Setenv("JWT_ISSUER", testIssuer)

	service, err := NewTokenService()
	if err != nil {
		t.Fatalf("NewTokenService returned error: %v", err)
	}

	tokenString := signAccessToken(t, testSecret, "27")
	claims, err := service.ParseAccessToken(context.Background(), tokenString)
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if claims.Subject != "27" {
		t.Fatalf("expected subject 27, got %q", claims.Subject)
	}
}

func TestNewTokenServiceRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		issuer string
	}{
		{"empty secret", "", testIssuer},
		{"placeholder secret", "your-access-token-secret", testIssuer},
		{"short secret", "too-short", testIssuer},
		{"empty issuer", testSecret, ""},
		{"placeholder issuer", testSecret, "your-app-name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JWT_ACCESS_TOKEN_SECRET", tc.secret)
			t.Setenv("JWT_ISSUER", tc.issuer)

			if _, err := NewTokenService(); err == nil {
				t.Fatal("NewTokenService accepted invalid config, want error")
			}
		})
	}
}

func TestNormalizeEnvValueStripsMatchingQuotes(t *testing.T) {
	if got := normalizeEnvValue(`"value"`); got != "value" {
		t.Fatalf("double-quoted value = %q, want value", got)
	}
	if got := normalizeEnvValue(`'value'`); got != "value" {
		t.Fatalf("single-quoted value = %q, want value", got)
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
