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

		templates := v1.Group("/templates")
		templates.Use(middleware.Authenticate(a.jwt))
		{
			templates.POST("", a.HandleCreateTaskTemplate)
			templates.GET("/:template_id", a.HandleGetTaskTemplate)
			templates.PATCH("/:template_id", a.HandleUpdateTaskTemplate)
			templates.DELETE("/:template_id", a.HandleArchiveTaskTemplate)
			templates.POST("/:template_id/attachments", a.HandleCreateTaskAttachment)
			templates.GET("/:template_id/attachments", a.HandleListTaskAttachments)
		}

		batches := v1.Group("/batches")
		batches.Use(middleware.Authenticate(a.jwt))
		{
			batches.POST("", a.HandleCreateTaskBatch)
			batches.GET("/:batch_id", a.HandleGetTaskBatch)
			batches.GET("/:batch_id/progress", a.HandleGetTaskBatchProgress)
			batches.GET("/:batch_id/instances", a.HandleListTaskBatchInstances)
			batches.POST("/:batch_id/comments", a.HandleCreateTaskBatchComment)
			batches.GET("/:batch_id/comments", a.HandleListTaskBatchComments)
			batches.POST("/:batch_id/attachments", a.HandleCreateTaskAttachment)
			batches.GET("/:batch_id/attachments", a.HandleListTaskAttachments)
		}

		instances := v1.Group("/instances")
		instances.Use(middleware.Authenticate(a.jwt))
		{
			instances.GET("/me", a.HandleListMyTaskInstances)
			instances.GET("/:instance_id", a.HandleGetTaskInstance)
			instances.PATCH("/:instance_id", a.HandleUpdateTaskInstance)
			instances.PATCH("/:instance_id/status", a.HandleUpdateTaskInstanceStatus)
			instances.GET("/:instance_id/events", a.HandleListTaskInstanceEvents)
			instances.POST("/:instance_id/dependencies", a.HandleCreateTaskInstanceDependency)
			instances.GET("/:instance_id/dependencies", a.HandleListTaskInstanceDependencies)
			instances.GET("/:instance_id/dependents", a.HandleListTaskInstanceDependents)
			instances.POST("/:instance_id/comments", a.HandleCreateTaskComment)
			instances.GET("/:instance_id/comments", a.HandleListTaskComments)
			instances.POST("/:instance_id/attachments", a.HandleCreateTaskAttachment)
			instances.GET("/:instance_id/attachments", a.HandleListTaskAttachments)
			instances.POST("/:instance_id/submissions", a.HandleCreateTaskSubmission)
			instances.GET("/:instance_id/submissions", a.HandleListTaskSubmissions)
			instances.PATCH("/:instance_id/submissions/:submission_id", a.HandleReviewTaskSubmission)
		}
	}

	return router
}
