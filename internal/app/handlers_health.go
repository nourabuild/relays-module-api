package app

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

type LivenessResponse struct {
	Status     string `json:"status"`
	Host       string `json:"host"`
	GOMAXPROCS int    `json:"gomaxprocs"`
}

// HandleReadiness reports whether the service can reach its database. The
// endpoint is public, so it exposes only up/down — pool statistics stay
// internal — and returns 503 when not ready so probes actually fail.
func (a *App) HandleReadiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)

	status := a.db.Health()["status"]
	code := http.StatusOK
	if status != "up" {
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, gin.H{"status": status})
}

func (a *App) HandleLiveness(c *gin.Context) {
	host, _ := os.Hostname()
	if host == "" {
		host = "unavailable"
	}

	c.JSON(http.StatusOK, LivenessResponse{
		Status:     "up",
		Host:       host,
		GOMAXPROCS: runtime.GOMAXPROCS(0),
	})
}
