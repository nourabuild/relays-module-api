package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLoggerRedactsTokenQueryParams(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Logger())
	router.GET("/ws", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ws?access_token=super-secret-token&room=42", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	logged := buf.String()
	if strings.Contains(logged, "super-secret-token") {
		t.Fatalf("access token leaked into log output: %s", logged)
	}
	if !strings.Contains(logged, "access_token=%5BREDACTED%5D") {
		t.Fatalf("expected redaction marker in log output: %s", logged)
	}
	if !strings.Contains(logged, "room=42") {
		t.Fatalf("expected non-sensitive params to be preserved: %s", logged)
	}
}

func TestRedactQuery(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"token param", "token=abc", "token=%5BREDACTED%5D"},
		{"mixed params", "q=hello&access_token=abc", "access_token=%5BREDACTED%5D&q=hello"},
		{"no sensitive params", "q=hello", "q=hello"},
		{"unparseable", "a=%zz;b", "[unparseable_query_redacted]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactQuery(tc.raw); got != tc.want {
				t.Fatalf("redactQuery(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
