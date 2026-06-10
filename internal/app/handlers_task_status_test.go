package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/jwt"
)

const (
	testJWTSecret = "test-secret-0f1e2d3c4b5a69788796a5b4c3d2e1f0aabbccdd"
	testIssuer    = "relays-test"

	testTaskID     = "8426a30f-c9e3-410b-82a4-b31bb3c4f97a"
	testCreatorID  = "creator-21"
	testAssigneeID = "assignee-27"
	testStrangerID = "stranger-99"
)

func setTestJWTEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_ACCESS_TOKEN_SECRET", testJWTSecret)
	t.Setenv("JWT_ISSUER", testIssuer)
}

// statusFakeDB serves a single task and applies status updates in memory.
type statusFakeDB struct {
	sqldb.Service
	task models.Task
}

func (db statusFakeDB) GetTask(ctx context.Context, taskID string) (models.Task, error) {
	if taskID != db.task.ID {
		return models.Task{}, sqldb.ErrDBNotFound
	}
	return db.task, nil
}

func (db statusFakeDB) UpdateTaskStatus(ctx context.Context, taskID, status string) (models.Task, error) {
	updated := db.task
	updated.Status = status
	return updated, nil
}

func (db statusFakeDB) UpdateTask(ctx context.Context, taskID string, input models.UpdateTask) (models.Task, error) {
	return db.task, nil
}

func newTestTokenService(t *testing.T) *jwt.TokenService {
	t.Helper()
	tokenService, err := jwt.NewTokenService()
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}
	return tokenService
}

func newStatusTestApp(t *testing.T, currentStatus string) *App {
	t.Helper()
	setTestJWTEnv(t)
	return NewApp(statusFakeDB{
		task: models.Task{
			ID:           testTaskID,
			CreatedByID:  testCreatorID,
			AssignedToID: testAssigneeID,
			Status:       currentStatus,
		},
	}, nil, newTestTokenService(t))
}

func patchTaskStatus(t *testing.T, app *App, actorID, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest(http.MethodPatch,
		"/api/v1/task/"+testTaskID+"/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+signTestAccessToken(t, testJWTSecret, actorID))

	rec := httptest.NewRecorder()
	app.RegisterRoutes().ServeHTTP(rec, req)
	return rec
}

// TestUpdateTaskStatusTransitionMatrix encodes the decided status state
// machine: from open, the assignee completes and the creator cancels; only
// the creator may reopen a terminal task; terminal-to-terminal moves
// conflict.
func TestUpdateTaskStatusTransitionMatrix(t *testing.T) {
	cases := []struct {
		name    string
		current string
		actor   string
		target  string
		want    int
	}{
		// From open.
		{"open: creator no-op reopen", models.TaskStatusOpen, testCreatorID, models.TaskStatusOpen, http.StatusOK},
		{"open: creator cannot complete", models.TaskStatusOpen, testCreatorID, models.TaskStatusDone, http.StatusForbidden},
		{"open: creator cancels", models.TaskStatusOpen, testCreatorID, models.TaskStatusCancelled, http.StatusOK},
		{"open: assignee cannot reopen", models.TaskStatusOpen, testAssigneeID, models.TaskStatusOpen, http.StatusForbidden},
		{"open: assignee completes", models.TaskStatusOpen, testAssigneeID, models.TaskStatusDone, http.StatusOK},
		{"open: assignee cannot cancel", models.TaskStatusOpen, testAssigneeID, models.TaskStatusCancelled, http.StatusForbidden},

		// From done.
		{"done: creator reopens", models.TaskStatusDone, testCreatorID, models.TaskStatusOpen, http.StatusOK},
		{"done: creator re-complete conflicts", models.TaskStatusDone, testCreatorID, models.TaskStatusDone, http.StatusConflict},
		{"done: creator cancel conflicts", models.TaskStatusDone, testCreatorID, models.TaskStatusCancelled, http.StatusConflict},
		{"done: assignee cannot reopen", models.TaskStatusDone, testAssigneeID, models.TaskStatusOpen, http.StatusForbidden},
		{"done: assignee re-complete conflicts", models.TaskStatusDone, testAssigneeID, models.TaskStatusDone, http.StatusConflict},
		{"done: assignee cancel conflicts", models.TaskStatusDone, testAssigneeID, models.TaskStatusCancelled, http.StatusConflict},

		// From cancelled.
		{"cancelled: creator reopens", models.TaskStatusCancelled, testCreatorID, models.TaskStatusOpen, http.StatusOK},
		{"cancelled: creator complete conflicts", models.TaskStatusCancelled, testCreatorID, models.TaskStatusDone, http.StatusConflict},
		{"cancelled: creator re-cancel conflicts", models.TaskStatusCancelled, testCreatorID, models.TaskStatusCancelled, http.StatusConflict},
		{"cancelled: assignee cannot reopen", models.TaskStatusCancelled, testAssigneeID, models.TaskStatusOpen, http.StatusForbidden},
		{"cancelled: assignee complete conflicts", models.TaskStatusCancelled, testAssigneeID, models.TaskStatusDone, http.StatusConflict},
		{"cancelled: assignee re-cancel conflicts", models.TaskStatusCancelled, testAssigneeID, models.TaskStatusCancelled, http.StatusConflict},

		// Strangers never get past task access, regardless of state.
		{"open: stranger forbidden", models.TaskStatusOpen, testStrangerID, models.TaskStatusDone, http.StatusForbidden},
		{"done: stranger forbidden", models.TaskStatusDone, testStrangerID, models.TaskStatusOpen, http.StatusForbidden},

		// Invalid status strings are rejected before any authz decision.
		{"open: invalid status", models.TaskStatusOpen, testCreatorID, "archived", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newStatusTestApp(t, tc.current)
			rec := patchTaskStatus(t, app, tc.actor, `{"status":"`+tc.target+`"}`)
			if rec.Code != tc.want {
				t.Fatalf("PATCH status %s→%s as %s: got %d, want %d (body: %s)",
					tc.current, tc.target, tc.actor, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestUpdateTaskRejectsTerminalTasks pins that field edits are only allowed
// while a task is open; terminal tasks must be reopened first.
func TestUpdateTaskRejectsTerminalTasks(t *testing.T) {
	for _, current := range []string{models.TaskStatusDone, models.TaskStatusCancelled} {
		t.Run(current, func(t *testing.T) {
			app := newStatusTestApp(t, current)

			req := httptest.NewRequest(http.MethodPatch,
				"/api/v1/task/"+testTaskID, strings.NewReader(`{"title":"new title"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+signTestAccessToken(t, testJWTSecret, testCreatorID))

			rec := httptest.NewRecorder()
			app.RegisterRoutes().ServeHTTP(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("PATCH task in %s state: got %d, want %d (body: %s)",
					current, rec.Code, http.StatusConflict, rec.Body.String())
			}
		})
	}
}

func TestGetTaskAuthorization(t *testing.T) {
	cases := []struct {
		name   string
		taskID string
		actor  string
		want   int
	}{
		{"creator reads task", testTaskID, testCreatorID, http.StatusOK},
		{"assignee reads task", testTaskID, testAssigneeID, http.StatusOK},
		{"stranger forbidden", testTaskID, testStrangerID, http.StatusForbidden},
		{"malformed id rejected", "not-a-uuid", testCreatorID, http.StatusBadRequest},
		{"unknown id not found", "00000000-0000-0000-0000-000000000000", testCreatorID, http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newStatusTestApp(t, models.TaskStatusOpen)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/task/"+tc.taskID, nil)
			req.Header.Set("Authorization", "Bearer "+signTestAccessToken(t, testJWTSecret, tc.actor))

			rec := httptest.NewRecorder()
			app.RegisterRoutes().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET task as %s: got %d, want %d (body: %s)",
					tc.actor, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
