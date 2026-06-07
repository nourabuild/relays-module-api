package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegisterRoutesSplitsTaskResources(t *testing.T) {
	t.Parallel()

	router := (&App{}).RegisterRoutes()
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	expectedRoutes := []string{
		http.MethodPost + " /api/v1/batches",
		http.MethodGet + " /api/v1/batches/:batch_id",
		http.MethodGet + " /api/v1/batches/:batch_id/progress",
		http.MethodGet + " /api/v1/batches/:batch_id/instances",
		http.MethodPost + " /api/v1/batches/:batch_id/instances",
		http.MethodPost + " /api/v1/batches/:batch_id/comments",
		http.MethodGet + " /api/v1/batches/:batch_id/comments",
		http.MethodPost + " /api/v1/batches/:batch_id/attachments",
		http.MethodGet + " /api/v1/batches/:batch_id/attachments",
		http.MethodGet + " /api/v1/instances/me",
		http.MethodGet + " /api/v1/instances/:instance_id",
		http.MethodPatch + " /api/v1/instances/:instance_id",
		http.MethodPatch + " /api/v1/instances/:instance_id/status",
		http.MethodDelete + " /api/v1/instances/:instance_id",
		http.MethodGet + " /api/v1/instances/:instance_id/events",
		http.MethodPost + " /api/v1/instances/:instance_id/dependencies",
		http.MethodGet + " /api/v1/instances/:instance_id/dependencies",
		http.MethodGet + " /api/v1/instances/:instance_id/dependents",
		http.MethodPost + " /api/v1/instances/:instance_id/comments",
		http.MethodGet + " /api/v1/instances/:instance_id/comments",
		http.MethodPost + " /api/v1/instances/:instance_id/attachments",
		http.MethodGet + " /api/v1/instances/:instance_id/attachments",
		http.MethodPost + " /api/v1/instances/:instance_id/submissions",
		http.MethodGet + " /api/v1/instances/:instance_id/submissions",
		http.MethodPatch + " /api/v1/instances/:instance_id/submissions/:submission_id",
	}

	for _, expected := range expectedRoutes {
		if !routes[expected] {
			t.Fatalf("missing route %s", expected)
		}
	}

	for route := range routes {
		if strings.Contains(route, " /api/v1/templates") {
			t.Fatalf("template route is still registered: %s", route)
		}
		if strings.Contains(route, " /api/v1/tasks") {
			t.Fatalf("legacy task route is still registered: %s", route)
		}
	}
}
