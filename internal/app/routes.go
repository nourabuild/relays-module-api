// Package app provides HTTP handlers for the IAM service.
package app

import (
	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
)

// ----------------------------------------------------------------------------
// Route Registration
// ----------------------------------------------------------------------------

func (a *App) RegisterRoutes() *gin.Engine {
	router := gin.New()

	// Global middleware chain
	router.Use(gin.Recovery())      // Panic recovery
	router.Use(middleware.Logger()) // Custom slog logger
	router.Use(middleware.CORS())   // CORS support

	// API v1 route group
	v1 := router.Group("/api/v1")
	{
		// Health check routes (public)
		health := v1.Group("/health")
		{
			health.GET("/readiness", a.HandleReadiness)
			health.GET("/liveness", a.HandleLiveness)
		}

		// User routes (protected - requires authentication)
		user := v1.Group("/user")
		user.Use(middleware.Authenticate(a.jwt))
		{
			user.GET("/me", a.HandleMe)
			user.GET("/search", a.HandleSearchUsers)
			user.GET("/:account/lookup", a.HandleAccountLookup)
		}

		task := v1.Group("/task")
		task.Use(middleware.Authenticate(a.jwt))
		{
			task.GET("/expectations", a.HandleListExpectations)
			task.GET("/todos", a.HandleListTodos)
			task.POST("", a.HandleCreateTask)
			task.GET("/:task_id", a.HandleGetTask)
			task.PATCH("/:task_id", a.HandleUpdateTask)
			task.PATCH("/:task_id/status", a.HandleUpdateTaskStatus)
		}

		chat := v1.Group("/chat")
		{
			chat.GET("/task/:task_id/ws", a.HandleTaskChatWebSocket)

			protectedChat := chat.Group("")
			protectedChat.Use(middleware.Authenticate(a.jwt))
			{
				protectedChat.GET("/task/:task_id/messages", a.HandleListTaskMessages)
				protectedChat.POST("/task/:task_id/messages", a.HandleCreateTaskMessage)
			}
		}
	}

	return router
}
