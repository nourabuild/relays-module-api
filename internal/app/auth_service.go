package app

import (
	"os"
	"strings"
)

const (
	defaultAuthAPIBaseURL = "https://api.auth.noura.software"
	authServiceMePath     = "/api/v1/user/me"
)

func authServiceMeURL() string {
	baseURL := firstEnvValue("AUTH_API_BASE_URL", "AUTH_SERVICE_BASE_URL", "AUTH_API_URL")
	if baseURL == "" {
		baseURL = defaultAuthAPIBaseURL
	}

	return strings.TrimRight(baseURL, "/") + authServiceMePath
}

func firstEnvValue(keys ...string) string {
	for _, key := range keys {
		if value := normalizeEnvValue(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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
