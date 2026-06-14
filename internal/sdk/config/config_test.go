package config

import (
	"strings"
	"testing"
)

const (
	validSecret = "test-secret-0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	validIssuer = "relays-test"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BLUEPRINT_DB_HOST", "localhost")
	t.Setenv("BLUEPRINT_DB_PORT", "5432")
	t.Setenv("BLUEPRINT_DB_USERNAME", "postgres")
	t.Setenv("BLUEPRINT_DB_PASSWORD", "postgres")
	t.Setenv("BLUEPRINT_DB_DATABASE", "noura_todos_db")
	t.Setenv("BLUEPRINT_DB_SCHEMA", "")
	t.Setenv("BLUEPRINT_DB_SSLMODE", "")
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", validSecret)
	t.Setenv("JWT_ISSUER", validIssuer)
	t.Setenv("AUTH_API_BASE_URL", "")
	t.Setenv("AUTH_SERVICE_BASE_URL", "")
	t.Setenv("AUTH_API_URL", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "")
	t.Setenv("PORT", "")
	t.Setenv("DEBUG_PORT", "")
}

func TestLoadAppliesDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.HTTP.Port != "10267" {
		t.Errorf("default port = %q, want 10267", cfg.HTTP.Port)
	}
	if cfg.DB.SSLMode != "require" {
		t.Errorf("default sslmode = %q, want require", cfg.DB.SSLMode)
	}
	if cfg.DB.Schema != "todos" {
		t.Errorf("default schema = %q, want todos", cfg.DB.Schema)
	}
	if cfg.Auth.MeURL != "https://api.auth.noura.software/api/v1/user/me" {
		t.Errorf("default auth me URL = %q", cfg.Auth.MeURL)
	}
}

func TestLoadUsesConfiguredAuthBaseURL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("AUTH_API_BASE_URL", "http://localhost:10067/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Auth.MeURL != "http://localhost:10067/api/v1/user/me" {
		t.Errorf("auth me URL = %q, want http://localhost:10067/api/v1/user/me", cfg.Auth.MeURL)
	}
}

func TestLoadNormalizesQuotedValues(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", `"`+validSecret+`"`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.JWT.Secret != validSecret {
		t.Errorf("quoted secret not normalized: %q", cfg.JWT.Secret)
	}
}

func TestLoadRejectsBadJWTConfig(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		issuer string
	}{
		{"empty secret", "", validIssuer},
		{"placeholder secret", "your-access-token-secret", validIssuer},
		{"short secret", "too-short", validIssuer},
		{"empty issuer", validSecret, ""},
		{"placeholder issuer", validSecret, "your-app-name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("JWT_ACCESS_TOKEN_SECRET", tc.secret)
			t.Setenv("JWT_ISSUER", tc.issuer)

			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted invalid JWT config, want error")
			}
		})
	}
}

func TestLoadReportsAllProblemsAtOnce(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BLUEPRINT_DB_HOST", "")
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted missing host and secret, want error")
	}
	for _, want := range []string{"BLUEPRINT_DB_HOST", "JWT_ACCESS_TOKEN_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadParsesCORS(t *testing.T) {
	setValidEnv(t)
	t.Setenv("CORS_ALLOW_ORIGINS", "https://a.example, https://b.example ,")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.CORS.AllowOrigins) != 2 || cfg.CORS.AllowOrigins[0] != "https://a.example" {
		t.Errorf("origins = %v", cfg.CORS.AllowOrigins)
	}
	if !cfg.CORS.AllowCredentials {
		t.Error("credentials should be enabled with explicit origins")
	}
}

func TestLoadDisablesCredentialsForWildcardOrigins(t *testing.T) {
	setValidEnv(t)
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.CORS.AllowCredentials {
		t.Error("credentials must be disabled when all origins are allowed")
	}
}
