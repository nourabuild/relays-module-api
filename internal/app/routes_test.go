package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegisterRoutesUsesMinimalTaskSurface(t *testing.T) {
	t.Parallel()

	router := (&App{}).RegisterRoutes()
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	expectedRoutes := []string{
		http.MethodGet + " /api/v1/task/expectations",
		http.MethodGet + " /api/v1/task/todos",
		http.MethodPost + " /api/v1/task",
		http.MethodGet + " /api/v1/task/:task_id",
		http.MethodPatch + " /api/v1/task/:task_id",
		http.MethodPatch + " /api/v1/task/:task_id/status",
		http.MethodGet + " /api/v1/chat/task/:task_id/messages",
		http.MethodPost + " /api/v1/chat/task/:task_id/messages",
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
		if strings.Contains(route, " /api/v1/batches") {
			t.Fatalf("batch route is still registered: %s", route)
		}
		if strings.Contains(route, " /api/v1/instances") {
			t.Fatalf("instance route is still registered: %s", route)
		}
	}
}
