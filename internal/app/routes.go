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

		tasks := v1.Group("")
		tasks.Use(middleware.Authenticate(a.jwt))
		{
			tasks.GET("/expectations", a.HandleListExpectations)
			tasks.GET("/todos", a.HandleListTodos)
			tasks.POST("/tasks", a.HandleCreateTask)
			tasks.GET("/tasks/:task_id", a.HandleGetTask)
			tasks.PATCH("/tasks/:task_id", a.HandleUpdateTask)
			tasks.PATCH("/tasks/:task_id/status", a.HandleUpdateTaskStatus)
			tasks.GET("/tasks/:task_id/messages", a.HandleListTaskMessages)
			tasks.POST("/tasks/:task_id/messages", a.HandleCreateTaskMessage)
		}
	}

	return router
}
