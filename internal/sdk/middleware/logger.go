package middleware

import (
	"log/slog"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

// sensitiveQueryParams are query parameters that carry credentials (e.g.
// WebSocket bearer tokens) and must never reach the logs.
var sensitiveQueryParams = []string{"access_token", "token"}

// redactQuery masks credential-bearing query parameters. If the query string
// cannot be parsed, it is dropped entirely rather than logged raw.
func redactQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[unparseable_query_redacted]"
	}

	for _, param := range sensitiveQueryParams {
		if values.Has(param) {
			values.Set(param, "[REDACTED]")
		}
	}

	return values.Encode()
}

// Logger returns a middleware that logs HTTP requests using slog
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate request duration
		duration := time.Since(start)

		// Build the full path, masking credential-bearing parameters
		if raw != "" {
			path = path + "?" + redactQuery(raw)
		}

		// Log the request details
		slog.Info("http request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", duration.Milliseconds(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"error", c.Errors.ByType(gin.ErrorTypePrivate).String(),
		)
	}
}
