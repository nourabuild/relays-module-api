package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nourabuild/relays-api/internal/sdk/config"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
)

type healthFakeDB struct {
	sqldb.Service
	status string
}

func (db healthFakeDB) Health() map[string]string {
	return map[string]string{
		"status":           db.status,
		"open_connections": "7",
		"wait_count":       "3",
	}
}

func TestReadinessExposesOnlyStatusAndFailsWhenDown(t *testing.T) {
	cases := []struct {
		status   string
		wantCode int
	}{
		{"up", http.StatusOK},
		{"down", http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			app := NewApp(config.Config{}, healthFakeDB{status: tc.status}, nil, newTestTokenService())

			req := httptest.NewRequest(http.MethodGet, "/api/v1/health/readiness", nil)
			rec := httptest.NewRecorder()
			app.RegisterRoutes().ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("readiness (%s) = %d, want %d", tc.status, rec.Code, tc.wantCode)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding readiness body: %v", err)
			}
			if body["status"] != tc.status {
				t.Errorf("status = %v, want %s", body["status"], tc.status)
			}
			if _, leaked := body["open_connections"]; leaked {
				t.Errorf("readiness leaks pool statistics: %v", body)
			}
		})
	}
}
