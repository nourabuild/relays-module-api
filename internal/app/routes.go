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
		}

		tasks := v1.Group("/tasks")
		tasks.Use(middleware.Authenticate(a.jwt))
		{
			tasks.GET("/me/instances", a.HandleListMyTaskInstances)

			tasks.POST("/templates", a.HandleCreateTaskTemplate)
			tasks.GET("/templates/:template_id", a.HandleGetTaskTemplate)
			tasks.PATCH("/templates/:template_id", a.HandleUpdateTaskTemplate)
			tasks.DELETE("/templates/:template_id", a.HandleArchiveTaskTemplate)
			tasks.POST("/templates/:template_id/attachments", a.HandleCreateTaskAttachment)
			tasks.GET("/templates/:template_id/attachments", a.HandleListTaskAttachments)

			tasks.POST("/batches", a.HandleCreateTaskBatch)
			tasks.GET("/batches/:batch_id", a.HandleGetTaskBatch)
			tasks.GET("/batches/:batch_id/progress", a.HandleGetTaskBatchProgress)
			tasks.GET("/batches/:batch_id/instances", a.HandleListTaskBatchInstances)
			tasks.POST("/batches/:batch_id/comments", a.HandleCreateTaskBatchComment)
			tasks.GET("/batches/:batch_id/comments", a.HandleListTaskBatchComments)
			tasks.POST("/batches/:batch_id/attachments", a.HandleCreateTaskAttachment)
			tasks.GET("/batches/:batch_id/attachments", a.HandleListTaskAttachments)

			tasks.GET("/instances/:task_instance_id", a.HandleGetTaskInstance)
			tasks.PATCH("/instances/:task_instance_id", a.HandleUpdateTaskInstance)
			tasks.PATCH("/instances/:task_instance_id/status", a.HandleUpdateTaskInstanceStatus)
			tasks.GET("/instances/:task_instance_id/events", a.HandleListTaskInstanceEvents)
			tasks.POST("/instances/:task_instance_id/comments", a.HandleCreateTaskComment)
			tasks.GET("/instances/:task_instance_id/comments", a.HandleListTaskComments)
			tasks.POST("/instances/:task_instance_id/attachments", a.HandleCreateTaskAttachment)
			tasks.GET("/instances/:task_instance_id/attachments", a.HandleListTaskAttachments)
			tasks.POST("/instances/:task_instance_id/submissions", a.HandleCreateTaskSubmission)
			tasks.GET("/instances/:task_instance_id/submissions", a.HandleListTaskSubmissions)
			tasks.PATCH("/submissions/:submission_id", a.HandleReviewTaskSubmission)
		}
	}

	return router
}
