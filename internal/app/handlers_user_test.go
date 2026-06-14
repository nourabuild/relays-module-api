package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nourabuild/relays-api/internal/sdk/config"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
)

type searchFakeDB struct {
	sqldb.Service
	users []models.User
}

func (db searchFakeDB) SearchUsers(ctx context.Context, query string) ([]models.User, error) {
	return db.users, nil
}

// TestSearchUsersHidesPII pins the decision that other users' profiles expose
// only public fields: no email, phone, DOB, city, or admin flag.
func TestSearchUsersHidesPII(t *testing.T) {
	phone := "+1-555-0100"
	dob := "1990-01-01"
	city := "Berlin"
	app := NewApp(config.Config{}, searchFakeDB{
		users: []models.User{{
			ID:      "27",
			Name:    "John Doe",
			Account: "johndoe",
			Email:   "john@example.com",
			Phone:   &phone,
			DOB:     &dob,
			City:    &city,
			IsAdmin: true,
		}},
	}, nil, newTestTokenService())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/search?q=john", nil)
	req.Header.Set("Authorization", "Bearer "+signTestAccessToken(t, testJWTSecret, testStrangerID))

	rec := httptest.NewRecorder()
	app.RegisterRoutes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("search returned %d: %s", rec.Code, rec.Body.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("decoding search response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	profile := results[0]
	for _, hidden := range []string{"email", "phone", "dob", "city", "is_admin"} {
		if _, ok := profile[hidden]; ok {
			t.Errorf("search result leaks %q: %v", hidden, profile)
		}
	}
	for _, visible := range []string{"id", "name", "account"} {
		if _, ok := profile[visible]; !ok {
			t.Errorf("search result missing public field %q: %v", visible, profile)
		}
	}
}
