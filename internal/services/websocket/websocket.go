// Package websocket provides per-task chat rooms over WebSocket connections.
package websocket

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

const (
	EventTaskChatConnected  = "task_chat.connected"
	EventTaskMessageCreated = "task_message.created"

	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1024
	sendBufferSize = 16
)

type Event struct {
	Type    string              `json:"type"`
	TaskID  string              `json:"task_id"`
	Message *models.TaskMessage `json:"message,omitempty"`
}

type Service struct {
	upgrader gorillawebsocket.Upgrader
	mu       sync.RWMutex
	rooms    map[string]map[*client]struct{}
}

type client struct {
	service *Service
	conn    *gorillawebsocket.Conn
	taskID  string
	send    chan Event
}

func NewService() *Service {
	allowedOrigins := parseOrigins(os.Getenv("CORS_ALLOW_ORIGINS"))

	return &Service{
		upgrader: gorillawebsocket.Upgrader{
			ReadBufferSize:  maxMessageSize,
			WriteBufferSize: maxMessageSize,
			CheckOrigin:     checkOrigin(allowedOrigins),
		},
		rooms: make(map[string]map[*client]struct{}),
	}
}

func (s *Service) ServeTask(w http.ResponseWriter, r *http.Request, taskID string) error {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	connClient := &client{
		service: s,
		conn:    conn,
		taskID:  taskID,
		send:    make(chan Event, sendBufferSize),
	}

	s.register(connClient)
	connClient.send <- Event{Type: EventTaskChatConnected, TaskID: taskID}

	go connClient.writePump()
	connClient.readPump()

	return nil
}

func (s *Service) BroadcastTaskMessage(taskID string, message models.TaskMessage) {
	event := Event{
		Type:    EventTaskMessageCreated,
		TaskID:  taskID,
		Message: &message,
	}

	var staleClients []*client

	s.mu.RLock()
	for client := range s.rooms[taskID] {
		select {
		case client.send <- event:
		default:
			staleClients = append(staleClients, client)
		}
	}
	s.mu.RUnlock()

	for _, client := range staleClients {
		s.unregister(client)
	}
}

func (s *Service) register(connClient *client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rooms[connClient.taskID] == nil {
		s.rooms[connClient.taskID] = make(map[*client]struct{})
	}
	s.rooms[connClient.taskID][connClient] = struct{}{}
}

func (s *Service) unregister(connClient *client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	room := s.rooms[connClient.taskID]
	if _, ok := room[connClient]; !ok {
		return
	}

	delete(room, connClient)
	close(connClient.send)
	if len(room) == 0 {
		delete(s.rooms, connClient.taskID)
	}
}

func (c *client) readPump() {
	defer c.service.unregister(c)

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(gorillawebsocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(gorillawebsocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func checkOrigin(allowedOrigins []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" || len(allowedOrigins) == 0 {
			return true
		}

		for _, allowedOrigin := range allowedOrigins {
			if allowedOrigin == "*" || strings.EqualFold(allowedOrigin, origin) {
				return true
			}
		}

		return false
	}
}

func parseOrigins(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}

	return origins
}
