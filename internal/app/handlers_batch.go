package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func (a *App) HandleCreateTaskBatch(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	var input models.CreateTaskBatch
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if details := validateCreateTaskBatch(input, userID); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_batch", details)
		return
	}

	result, err := a.db.CreateTaskBatch(c.Request.Context(), userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_batch", err)
		return
	}

	c.JSON(http.StatusCreated, result)
}

func (a *App) HandleGetTaskBatch(c *gin.Context) {
	batch, ok := a.getAuthorizedTaskBatch(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, batch)
}

func (a *App) HandleGetTaskBatchProgress(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	progress, err := a.db.GetTaskBatchProgress(c.Request.Context(), c.Param("batch_id"), true)
	if err != nil {
		a.writeTaskDBError(c, "get_task_batch_progress", err)
		return
	}
	if !canManageTaskBatch(progress.CreatedBy, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	c.JSON(http.StatusOK, progress)
}

func (a *App) HandleListTaskBatchInstances(c *gin.Context) {
	batch, ok := a.getAuthorizedTaskBatch(c)
	if !ok {
		return
	}

	instances, err := a.db.ListTaskBatchInstances(c.Request.Context(), batch.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_batch_instances", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"instances": instances})
}

func (a *App) HandleAddTaskBatchInstance(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	batch, err := a.db.GetTaskBatch(c.Request.Context(), c.Param("batch_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_batch", err)
		return
	}

	canParticipate, err := a.canParticipateInTaskBatch(c.Request.Context(), batch, userID)
	if err != nil {
		a.writeTaskDBError(c, "check_task_batch_assignee", err)
		return
	}
	if !canParticipate {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.CreateTaskInstanceInBatch
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if details := validateCreateTaskInstanceInBatch(input, userID); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_instance", details)
		return
	}

	instance, err := a.db.AddTaskBatchInstance(c.Request.Context(), batch.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "add_task_batch_instance", err)
		return
	}

	c.JSON(http.StatusCreated, instance)
}

func (a *App) HandleCreateTaskBatchComment(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	batch, ok := a.getAuthorizedTaskBatch(c)
	if !ok {
		return
	}

	var input models.CreateTaskBatchComment
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if strings.TrimSpace(input.Body) == "" {
		writeError(c, http.StatusBadRequest, "invalid_task_batch_comment", map[string]string{"body": "body cannot be empty"})
		return
	}

	comment, err := a.db.CreateTaskBatchComment(c.Request.Context(), batch.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_batch_comment", err)
		return
	}

	c.JSON(http.StatusCreated, comment)
}

func (a *App) HandleListTaskBatchComments(c *gin.Context) {
	batch, ok := a.getAuthorizedTaskBatch(c)
	if !ok {
		return
	}

	comments, err := a.db.ListTaskBatchComments(c.Request.Context(), batch.ID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_batch_comments", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"comments": comments})
}

func (a *App) getAuthorizedTaskBatch(c *gin.Context) (models.TaskBatch, bool) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return models.TaskBatch{}, false
	}

	batch, err := a.db.GetTaskBatch(c.Request.Context(), c.Param("batch_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_batch", err)
		return models.TaskBatch{}, false
	}
	canParticipate, err := a.canParticipateInTaskBatch(c.Request.Context(), batch, userID)
	if err != nil {
		a.writeTaskDBError(c, "check_task_batch_assignee", err)
		return models.TaskBatch{}, false
	}
	if !canParticipate {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return models.TaskBatch{}, false
	}

	return batch, true
}

func validateCreateTaskBatch(input models.CreateTaskBatch, creatorID string) map[string]string {
	details := map[string]string{}

	if strings.TrimSpace(input.Title) == "" {
		details["title"] = "title is required"
	}
	for key, value := range validateTaskAssigneeInputs(input.Assignees, creatorID) {
		details[key] = value
	}
	return details
}

func validateCreateTaskInstanceInBatch(input models.CreateTaskInstanceInBatch, creatorID string) map[string]string {
	details := map[string]string{}
	if strings.TrimSpace(input.Title) == "" {
		details["title"] = "title is required"
	}
	for key, value := range validateTaskAssigneeInputs(input.Assignees, creatorID) {
		details[key] = value
	}
	return details
}

func validateTaskAssigneeInputs(inputs []models.TaskAssigneeInput, creatorID string) map[string]string {
	details := map[string]string{}
	if len(inputs) == 0 {
		details["assignees"] = "at least one assignee is required"
		return details
	}

	seenUserIDs := map[string]bool{}
	for index, assignee := range inputs {
		prefix := "assignees[" + strconv.Itoa(index) + "]."
		userID := strings.TrimSpace(assignee.UserID)
		if userID == "" {
			details[prefix+"user_id"] = "user_id is required"
			continue
		}
		if userID == creatorID {
			details[prefix+"user_id"] = "creator is added automatically as assigned_by"
		}
		if seenUserIDs[userID] {
			details[prefix+"user_id"] = "user_id must be unique"
		}
		seenUserIDs[userID] = true
	}
	return details
}

func canManageTaskBatch(createdBy, userID string) bool {
	return createdBy == userID
}

// canParticipateInTaskBatch decides who may act within an existing batch: read
// it, append a task to it, or wire dependencies between its instances. The batch
// creator always may; so may anyone already assigned a task within the batch,
// which is what lets a delegation chain continue (A assigns B, B appends a task
// for C, C may then append for D, and so on). The assignee lookup only runs when
// the cheaper creator check fails.
func (a *App) canParticipateInTaskBatch(ctx context.Context, batch models.TaskBatch, userID string) (bool, error) {
	if canManageTaskBatch(batch.CreatedBy, userID) {
		return true, nil
	}
	return a.db.IsTaskBatchAssignee(ctx, batch.ID, userID)
}
