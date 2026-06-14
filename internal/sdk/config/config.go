// Package config loads and validates all environment configuration in one
// place. Load fails fast and reports every problem at once, so a
// misconfigured service never starts half-working.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	defaultAuthAPIBaseURL = "https://api.auth.noura.software"
	authServiceMePath     = "/api/v1/user/me"

	// minJWTSecretLength rejects secrets too short to resist offline brute
	// force of HS256 signatures.
	minJWTSecretLength = 32
)

// placeholderJWTSecrets are template defaults that must never reach
// production; accepting one would let anyone who has seen the template forge
// tokens.
var placeholderJWTSecrets = map[string]struct{}{
	"your-access-token-secret":  {},
	"your-refresh-token-secret": {},
	"changeme":                  {},
	"secret":                    {},
}

type Config struct {
	HTTP   HTTP
	DB     DB
	JWT    JWT
	CORS   CORS
	Auth   Auth
	Sentry Sentry
}

type HTTP struct {
	Port      string
	DebugPort string
}

type DB struct {
	Host     string
	Port     string
	Username string
	Password string
	Database string
	Schema   string
	SSLMode  string
}

type JWT struct {
	Secret string
	Issuer string
}

type CORS struct {
	AllowOrigins     []string
	AllowCredentials bool
}

type Auth struct {
	BaseURL string
	// MeURL is the fully-resolved auth-service endpoint used to mirror
	// users locally on first request.
	MeURL string
}

type Sentry struct {
	DSN         string
	Environment string
}

// Load reads, normalizes, and validates all configuration from the
// environment. It returns every validation problem joined into one error.
func Load() (Config, error) {
	var problems []error

	cfg := Config{
		HTTP: HTTP{
			Port:      envOr("PORT", "10267"),
			DebugPort: envOr("DEBUG_PORT", "4000"),
		},
		DB: DB{
			Host:     env("BLUEPRINT_DB_HOST"),
			Port:     env("BLUEPRINT_DB_PORT"),
			Username: env("BLUEPRINT_DB_USERNAME"),
			Password: env("BLUEPRINT_DB_PASSWORD"),
			Database: env("BLUEPRINT_DB_DATABASE"),
			Schema:   envOr("BLUEPRINT_DB_SCHEMA", "todos"),
			// Encrypted connections by default; cleartext requires an
			// explicit opt-out.
			SSLMode: envOr("BLUEPRINT_DB_SSLMODE", "require"),
		},
		JWT: JWT{
			Secret: env("JWT_ACCESS_TOKEN_SECRET"),
			Issuer: env("JWT_ISSUER"),
		},
		CORS: CORS{
			AllowOrigins:     splitList(env("CORS_ALLOW_ORIGINS")),
			AllowCredentials: strings.EqualFold(env("CORS_ALLOW_CREDENTIALS"), "true"),
		},
		Sentry: Sentry{
			DSN:         env("SENTRY_DSN"),
			Environment: envOr("SENTRY_ENVIRONMENT", "development"),
		},
	}

	// Required database settings.
	for key, value := range map[string]string{
		"BLUEPRINT_DB_HOST":     cfg.DB.Host,
		"BLUEPRINT_DB_PORT":     cfg.DB.Port,
		"BLUEPRINT_DB_USERNAME": cfg.DB.Username,
		"BLUEPRINT_DB_DATABASE": cfg.DB.Database,
	} {
		if value == "" {
			problems = append(problems, fmt.Errorf("%s is required", key))
		}
	}

	// JWT settings must be real values shared with the auth service.
	if _, isPlaceholder := placeholderJWTSecrets[cfg.JWT.Secret]; cfg.JWT.Secret == "" || isPlaceholder {
		problems = append(problems, errors.New("JWT_ACCESS_TOKEN_SECRET must be set to the secret shared with the auth service (placeholder or empty value rejected)"))
	} else if len(cfg.JWT.Secret) < minJWTSecretLength {
		problems = append(problems, fmt.Errorf("JWT_ACCESS_TOKEN_SECRET must be at least %d characters, got %d", minJWTSecretLength, len(cfg.JWT.Secret)))
	}
	if cfg.JWT.Issuer == "" || cfg.JWT.Issuer == "your-app-name" {
		problems = append(problems, errors.New("JWT_ISSUER must be set to the issuer used by the auth service"))
	}

	// Credentialed CORS with a wildcard origin is rejected by browsers and
	// hides misconfiguration; disable credentials instead of failing.
	if cfg.CORS.AllowCredentials && len(cfg.CORS.AllowOrigins) == 0 {
		cfg.CORS.AllowCredentials = false
	}

	// Auth service base URL with legacy fallbacks.
	cfg.Auth.BaseURL = firstNonEmpty(
		env("AUTH_API_BASE_URL"),
		env("AUTH_SERVICE_BASE_URL"),
		env("AUTH_API_URL"),
		defaultAuthAPIBaseURL,
	)
	cfg.Auth.MeURL = strings.TrimRight(cfg.Auth.BaseURL, "/") + authServiceMePath

	if len(problems) > 0 {
		return Config{}, errors.Join(problems...)
	}
	return cfg, nil
}

// env reads an environment variable, trimming whitespace and one matching
// pair of surrounding quotes (a common .env file artifact).
func env(key string) string {
	return normalize(os.Getenv(key))
}

func envOr(key, fallback string) string {
	if value := env(key); value != "" {
		return value
	}
	return fallback
}

func normalize(value string) string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// splitList parses a comma-separated list, dropping empty entries.
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}

	return items
}
