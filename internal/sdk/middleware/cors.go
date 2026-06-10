package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/config"
)

// CORS returns a configured CORS middleware for Gin. An empty origin list
// allows all origins; config.Load already disables credentials in that case.
func CORS(cfg config.CORS) gin.HandlerFunc {
	allowOrigins := cfg.AllowOrigins
	if len(allowOrigins) == 0 {
		allowOrigins = []string{"*"}
	}

	return cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           12 * time.Hour,
	})
}
