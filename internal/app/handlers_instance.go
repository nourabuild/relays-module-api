package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/sentry"
)

func (a *App) HandleListMyTaskInstances(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	filter, details := parseTaskInstanceFilter(c)
	if len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_filter", details)
		return
	}

	instances, err := a.db.ListTaskInstancesForUser(c.Request.Context(), userID, filter)
	if err != nil {
		a.writeTaskDBError(c, "list_my_task_instances", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

func (a *App) HandleGetTaskInstance(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, instance)
}

func (a *App) HandleUpdateTaskInstance(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	var input models.UpdateTaskInstance
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if details := validateUpdateTaskInstance(input); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_instance", details)
		return
	}

	isAdmin := currentUserIsAdmin(c)
	if !canManageTaskInstance(instance, userID, isAdmin) && !(instance.AssigneeID == userID && assigneeCanPatchInstance(input)) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	updated, err := a.db.UpdateTaskInstance(c.Request.Context(), instance.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "update_task_instance", err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) HandleUpdateTaskInstanceStatus(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	var input models.UpdateTaskInstanceStatus
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if !models.IsValidTaskStatus(input.Status) {
		writeError(c, http.StatusBadRequest, "invalid_task_status", map[string]string{"status": "unsupported status"})
		return
	}
	if input.Status == models.TaskStatusPendingReview {
		writeError(c, http.StatusBadRequest, "invalid_task_status", map[string]string{"status": "set status to completed to request review"})
		return
	}

	isAdmin := currentUserIsAdmin(c)
	canManage := canManageTaskInstance(instance, userID, isAdmin)
	isAssignee := instance.AssigneeID == userID

	switch input.Status {
	case models.TaskStatusInProgress, models.TaskStatusCompleted:
		if !isAssignee {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	case models.TaskStatusCancelled:
		if !canManage {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	default:
		if !canManage && !isAssignee {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	}

	if input.Status == models.TaskStatusCompleted && instance.ReviewRequired {
		updated, err := a.db.SubmitTaskForReview(c.Request.Context(), instance.ID, userID, models.SubmitTaskReview{
			Note: input.CompletionNote,
		})
		if err != nil {
			a.writeTaskDBError(c, "submit_task_for_review", err)
			return
		}

		c.JSON(http.StatusOK, updated)
		return
	}

	updated, err := a.db.UpdateTaskInstanceStatus(c.Request.Context(), instance.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "update_task_instance_status", err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) HandleListTaskInstanceEvents(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	events, err := a.db.ListTaskInstanceEvents(c.Request.Context(), instance.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_instance_events", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (a *App) HandleCreateTaskInstanceDependency(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}
	if !canManageTaskInstance(instance, userID, currentUserIsAdmin(c)) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.CreateTaskInstanceDependency
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if details := validateCreateTaskInstanceDependency(input); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_dependency", details)
		return
	}

	dependsOnInstance, err := a.db.GetTaskInstance(c.Request.Context(), input.DependsOnTaskInstanceID)
	if err != nil {
		a.writeTaskDBError(c, "get_task_dependency_target", err)
		return
	}
	if dependsOnInstance.BatchID != instance.BatchID {
		writeError(c, http.StatusBadRequest, "invalid_task_dependency", map[string]string{"depends_on_task_instance_id": "dependency must be in the same batch"})
		return
	}

	dependency, err := a.db.CreateTaskInstanceDependency(c.Request.Context(), instance.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_instance_dependency", err)
		return
	}

	c.JSON(http.StatusCreated, dependency)
}

func (a *App) HandleListTaskInstanceDependencies(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	dependencies, err := a.db.ListTaskInstanceDependencies(c.Request.Context(), instance.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_instance_dependencies", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"dependencies": dependencies})
}

func (a *App) HandleListTaskInstanceDependents(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	dependents, err := a.db.ListTaskInstanceDependents(c.Request.Context(), instance.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_instance_dependents", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"dependents": dependents})
}

func (a *App) HandleCreateTaskComment(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	var input models.CreateTaskComment
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if strings.TrimSpace(input.Body) == "" {
		writeError(c, http.StatusBadRequest, "invalid_task_comment", map[string]string{"body": "body cannot be empty"})
		return
	}

	comment, err := a.db.CreateTaskComment(c.Request.Context(), instance.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_comment", err)
		return
	}

	c.JSON(http.StatusCreated, comment)
}

func (a *App) HandleListTaskComments(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	comments, err := a.db.ListTaskComments(c.Request.Context(), instance.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_comments", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"comments": comments})
}

func (a *App) HandleCreateTaskSubmission(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}
	if instance.AssigneeID != userID && !currentUserIsAdmin(c) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.CreateTaskSubmission
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}

	submission, err := a.db.CreateTaskSubmission(c.Request.Context(), instance.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_submission", err)
		return
	}

	c.JSON(http.StatusCreated, submission)
}

func (a *App) HandleListTaskSubmissions(c *gin.Context) {
	instance, ok := a.getAuthorizedTaskInstance(c)
	if !ok {
		return
	}

	submissions, err := a.db.ListTaskSubmissions(c.Request.Context(), instance.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_submissions", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"submissions": submissions})
}

func (a *App) HandleReviewTaskSubmission(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	submission, err := a.db.GetTaskSubmission(c.Request.Context(), c.Param("submission_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_submission_for_review", err)
		return
	}

	instance, err := a.db.GetTaskInstance(c.Request.Context(), submission.TaskInstanceID)
	if err != nil {
		a.writeTaskDBError(c, "get_task_instance_for_submission_review", err)
		return
	}
	if pathInstanceID := c.Param("instance_id"); pathInstanceID != "" && instance.ID != pathInstanceID {
		writeError(c, http.StatusNotFound, "not_found", nil)
		return
	}
	if !canManageTaskInstance(instance, userID, currentUserIsAdmin(c)) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.ReviewTaskSubmission
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if !models.IsValidSubmissionReviewStatus(input.Status) {
		writeError(c, http.StatusBadRequest, "invalid_submission_status", map[string]string{"status": "status must be accepted, rejected, or revision_requested"})
		return
	}

	reviewed, err := a.db.ReviewTaskSubmission(c.Request.Context(), submission.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "review_task_submission", err)
		return
	}

	c.JSON(http.StatusOK, reviewed)
}

func (a *App) HandleCreateTaskAttachment(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	scope, targetID, ok := a.authorizeAttachmentScope(c, true)
	if !ok {
		return
	}

	var input models.CreateTaskAttachment
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if strings.TrimSpace(input.FileURL) == "" {
		writeError(c, http.StatusBadRequest, "invalid_task_attachment", map[string]string{"file_url": "file_url cannot be empty"})
		return
	}
	if input.SizeBytes != nil && *input.SizeBytes < 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_attachment", map[string]string{"size_bytes": "size_bytes cannot be negative"})
		return
	}

	attachment, err := a.db.CreateTaskAttachment(c.Request.Context(), scope, targetID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_attachment", err)
		return
	}

	c.JSON(http.StatusCreated, attachment)
}

func (a *App) HandleListTaskAttachments(c *gin.Context) {
	scope, targetID, ok := a.authorizeAttachmentScope(c, false)
	if !ok {
		return
	}

	attachments, err := a.db.ListTaskAttachments(c.Request.Context(), scope, targetID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_attachments", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"attachments": attachments})
}

func (a *App) getAuthorizedTaskInstance(c *gin.Context) (models.TaskInstance, bool) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return models.TaskInstance{}, false
	}

	instance, err := a.db.GetTaskInstance(c.Request.Context(), c.Param("instance_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_instance", err)
		return models.TaskInstance{}, false
	}
	if !canAccessTaskInstance(instance, userID, currentUserIsAdmin(c)) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return models.TaskInstance{}, false
	}

	return instance, true
}

func (a *App) authorizeAttachmentScope(c *gin.Context, write bool) (string, string, bool) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return "", "", false
	}
	isAdmin := currentUserIsAdmin(c)

	if templateID := c.Param("template_id"); templateID != "" {
		template, err := a.db.GetTaskTemplate(c.Request.Context(), templateID)
		if err != nil {
			a.writeTaskDBError(c, "get_task_template_for_attachment", err)
			return "", "", false
		}
		if !canManageTaskTemplate(template, userID, isAdmin) {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return "", "", false
		}
		return models.AttachmentScopeTemplate, template.ID, true
	}

	if batchID := c.Param("batch_id"); batchID != "" {
		batch, err := a.db.GetTaskBatch(c.Request.Context(), batchID)
		if err != nil {
			a.writeTaskDBError(c, "get_task_batch_for_attachment", err)
			return "", "", false
		}
		if !canManageTaskBatch(batch.CreatedBy, userID, isAdmin) {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return "", "", false
		}
		return models.AttachmentScopeBatch, batch.ID, true
	}

	instanceID := c.Param("instance_id")
	instance, err := a.db.GetTaskInstance(c.Request.Context(), instanceID)
	if err != nil {
		a.writeTaskDBError(c, "get_task_instance_for_attachment", err)
		return "", "", false
	}
	if write {
		if !canAccessTaskInstance(instance, userID, isAdmin) {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return "", "", false
		}
	} else if !canAccessTaskInstance(instance, userID, isAdmin) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return "", "", false
	}

	return models.AttachmentScopeInstance, instance.ID, true
}

func (a *App) writeTaskDBError(c *gin.Context, handler string, err error) {
	switch {
	case errors.Is(err, sqldb.ErrDBNotFound):
		writeError(c, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, sqldb.ErrDBDuplicatedEntry):
		writeError(c, http.StatusConflict, "duplicate_resource", nil)
	case errors.Is(err, sqldb.ErrTaskDependencyCycle):
		writeError(c, http.StatusConflict, "task_dependency_cycle", nil)
	case errors.Is(err, sqldb.ErrTaskBlockedByDeps):
		writeError(c, http.StatusConflict, "task_blocked_by_dependencies", nil)
	case errors.Is(err, sqldb.ErrForeignKeyViolation):
		writeError(c, http.StatusBadRequest, "invalid_reference", nil)
	case errors.Is(err, sqldb.ErrCheckViolation), errors.Is(err, sqldb.ErrNotNullViolation):
		writeError(c, http.StatusBadRequest, "invalid_value", nil)
	default:
		a.toSentry(c, handler, "db", sentry.LevelError, err)
		writeError(c, http.StatusInternalServerError, "internal_error", nil)
	}
}

func validateCreateTaskInstanceDependency(input models.CreateTaskInstanceDependency) map[string]string {
	details := map[string]string{}
	if strings.TrimSpace(input.DependsOnTaskInstanceID) == "" {
		details["depends_on_task_instance_id"] = "depends_on_task_instance_id is required"
	}
	if input.DependencyType != nil && !models.IsValidTaskDependencyType(*input.DependencyType) {
		details["dependency_type"] = "dependency_type must be blocks_start or blocks_completion"
	}
	return details
}

func validateUpdateTaskInstance(input models.UpdateTaskInstance) map[string]string {
	details := map[string]string{}
	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		details["title"] = "title cannot be empty"
	}
	if input.ProgressPercent != nil && (*input.ProgressPercent < 0 || *input.ProgressPercent > 100) {
		details["progress_percent"] = "progress_percent must be between 0 and 100"
	}
	return details
}

func parseTaskInstanceFilter(c *gin.Context) (models.TaskInstanceFilter, map[string]string) {
	var filter models.TaskInstanceFilter
	details := map[string]string{}

	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status == "open" {
			filter.OpenOnly = true
		} else if models.IsValidTaskStatus(status) {
			filter.Status = &status
		} else {
			details["status"] = "unsupported status"
		}
	}

	if dueBefore := strings.TrimSpace(c.Query("due_before")); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			details["due_before"] = "due_before must be RFC3339"
		} else {
			filter.DueBefore = &parsed
		}
	}

	return filter, details
}

func currentUserIsAdmin(c *gin.Context) bool {
	value, exists := c.Get(middleware.IsAdminKey)
	if !exists {
		return false
	}
	isAdmin, ok := value.(bool)
	return ok && isAdmin
}

func canAccessTaskInstance(instance models.TaskInstance, userID string, isAdmin bool) bool {
	return isAdmin || instance.CreatedBy == userID || instance.AssigneeID == userID
}

func canManageTaskInstance(instance models.TaskInstance, userID string, isAdmin bool) bool {
	return isAdmin || instance.CreatedBy == userID
}

func assigneeCanPatchInstance(input models.UpdateTaskInstance) bool {
	return input.Title == nil &&
		input.Description == nil &&
		input.Instructions == nil &&
		input.Priority == nil &&
		input.DueAt == nil &&
		input.ReviewRequired == nil &&
		(input.ProgressPercent != nil || input.CustomPayload != nil)
}
