package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/jwt"
)

func TestTaskChatWebSocketRouteUpgradesAuthorizedTask(t *testing.T) {
	const (
		secret = testJWTSecret
		taskID = "8426a30f-c9e3-410b-82a4-b31bb3c4f97a"
		userID = "21"
	)

	setTestJWTEnv(t)

	app := NewApp(taskChatWebSocketDB{
		task: models.Task{
			ID:           taskID,
			CreatedByID:  userID,
			AssignedToID: "27",
		},
	}, nil, newTestTokenService(t))

	server := httptest.NewServer(app.RegisterRoutes())
	t.Cleanup(server.Close)

	token := signTestAccessToken(t, secret, userID)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/api/v1/chat/task/" + taskID + "/ws?access_token=" + token

	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket route: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var event struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read websocket connected event: %v", err)
	}
	if event.Type != "task_chat.connected" || event.TaskID != taskID {
		t.Fatalf("unexpected websocket event: %#v", event)
	}
}

func signTestAccessToken(t *testing.T, secret, subject string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.Claims{
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
		t.Fatalf("sign access token: %v", err)
	}
	return tokenString
}

type taskChatWebSocketDB struct {
	sqldb.Service
	task models.Task
}

func (db taskChatWebSocketDB) GetTask(ctx context.Context, taskID string) (models.Task, error) {
	if taskID != db.task.ID {
		return models.Task{}, sqldb.ErrDBNotFound
	}
	return db.task, nil
}
