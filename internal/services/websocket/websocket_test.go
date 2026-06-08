package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func TestBroadcastTaskMessageSendsToTaskSubscribers(t *testing.T) {
	t.Parallel()

	service := NewService()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := service.ServeTask(w, r, "task-1"); err != nil {
			t.Logf("serve websocket: %v", err)
		}
	}))
	defer server.Close()

	conn, _, err := gorillawebsocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))

	var connected Event
	if err := conn.ReadJSON(&connected); err != nil {
		t.Fatalf("read connected event: %v", err)
	}
	if connected.Type != EventTaskChatConnected || connected.TaskID != "task-1" {
		t.Fatalf("unexpected connected event: %#v", connected)
	}

	service.BroadcastTaskMessage("task-1", models.TaskMessage{
		ID:       "message-1",
		TaskID:   "task-1",
		AuthorID: "user-1",
		Body:     "hello",
	})

	var event Event
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read broadcast event: %v", err)
	}
	if event.Type != EventTaskMessageCreated || event.TaskID != "task-1" {
		t.Fatalf("unexpected broadcast event: %#v", event)
	}
	if event.Message == nil || event.Message.ID != "message-1" || event.Message.Body != "hello" {
		t.Fatalf("unexpected message payload: %#v", event.Message)
	}
}
