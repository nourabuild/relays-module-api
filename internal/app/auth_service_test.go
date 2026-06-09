package app

import "testing"

func TestAuthServiceMeURLUsesConfiguredBaseURL(t *testing.T) {
	t.Setenv("AUTH_API_BASE_URL", "http://localhost:10067/")
	t.Setenv("AUTH_SERVICE_BASE_URL", "")
	t.Setenv("AUTH_API_URL", "")

	got := authServiceMeURL()
	want := "http://localhost:10067/api/v1/user/me"
	if got != want {
		t.Fatalf("authServiceMeURL() = %q, want %q", got, want)
	}
}

func TestAuthServiceMeURLDefaultsToProduction(t *testing.T) {
	t.Setenv("AUTH_API_BASE_URL", "")
	t.Setenv("AUTH_SERVICE_BASE_URL", "")
	t.Setenv("AUTH_API_URL", "")

	got := authServiceMeURL()
	want := "https://api.auth.noura.software/api/v1/user/me"
	if got != want {
		t.Fatalf("authServiceMeURL() = %q, want %q", got, want)
	}
}
