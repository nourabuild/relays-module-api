package app

import (
	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/services/sentry"
)

func writeError(c *gin.Context, status int, errCode string, details map[string]string) {
	response := gin.H{
		"error": errCode,
	}

	if len(details) > 0 {
		response["details"] = details
	}

	c.JSON(status, response)
}

// =============================================================================
func (a *App) toSentry(c *gin.Context, handler, errType string, level sentry.Level, err error) {
	a.sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("handler", handler)
		scope.SetTag("error_type", errType)
		scope.SetLevel(level)
		if reqID := c.GetHeader("X-Request-ID"); reqID != "" {
			scope.SetTag("request_id", reqID)
		}
		a.sentry.CaptureException(err)
	})
}
