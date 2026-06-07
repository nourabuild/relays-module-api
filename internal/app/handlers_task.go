package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/sdk/sqldb"
	"github.com/nourabuild/relays-api/internal/services/sentry"
)

func (a *App) HandleListExpectations(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	tasks, err := a.db.ListExpectations(c.Request.Context(), userID)
	if err != nil {
		a.writeTaskDBError(c, "list_expectations", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": expectationItems(tasks)})
}

func (a *App) HandleListTodos(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	tasks, err := a.db.ListTodos(c.Request.Context(), userID)
	if err != nil {
		a.writeTaskDBError(c, "list_todos", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": todoItems(tasks)})
}

func (a *App) HandleCreateTask(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	var input models.CreateTask
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	normalizeCreateTaskInput(&input)
	if details := validateCreateTask(input, userID); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task", details)
		return
	}
	if !a.canUseDelegatedFromTask(c, input.DelegatedFromTaskID, userID) {
		return
	}

	task, err := a.db.CreateTask(c.Request.Context(), userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task", err)
		return
	}

	c.JSON(http.StatusCreated, task)
}

func (a *App) HandleGetTask(c *gin.Context) {
	task, _, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, task)
}

func (a *App) HandleUpdateTask(c *gin.Context) {
	task, userID, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}
	if task.CreatedByID != userID {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.UpdateTask
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	normalizeUpdateTaskInput(&input)
	if details := validateUpdateTask(input, task.CreatedByID); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task", details)
		return
	}
	if !a.canUseDelegatedFromTask(c, input.DelegatedFromTaskID, userID) {
		return
	}

	updated, err := a.db.UpdateTask(c.Request.Context(), task.ID, input)
	if err != nil {
		a.writeTaskDBError(c, "update_task", err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) HandleUpdateTaskStatus(c *gin.Context) {
	task, userID, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}

	var input models.UpdateTaskStatus
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	if !models.IsValidTaskStatus(input.Status) {
		writeError(c, http.StatusBadRequest, "invalid_task_status", map[string]string{"status": "status must be open, done, or cancelled"})
		return
	}

	switch input.Status {
	case models.TaskStatusDone:
		if task.AssignedToID != userID {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	case models.TaskStatusCancelled:
		if task.CreatedByID != userID {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	case models.TaskStatusOpen:
		if !canAccessTask(task, userID) {
			writeError(c, http.StatusForbidden, "forbidden", nil)
			return
		}
	}

	updated, err := a.db.UpdateTaskStatus(c.Request.Context(), task.ID, input.Status)
	if err != nil {
		a.writeTaskDBError(c, "update_task_status", err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) HandleListTaskMessages(c *gin.Context) {
	task, userID, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}

	messages, err := a.db.ListTaskMessages(c.Request.Context(), task.ID, userID)
	if err != nil {
		a.writeTaskDBError(c, "list_task_messages", err)
		return
	}
	if messages == nil {
		messages = []models.TaskMessage{}
	}

	c.JSON(http.StatusOK, gin.H{"items": messages})
}

func (a *App) HandleCreateTaskMessage(c *gin.Context) {
	task, userID, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}

	var input models.CreateTaskMessage
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		writeError(c, http.StatusBadRequest, "invalid_task_message", map[string]string{"body": "body is required"})
		return
	}

	message, err := a.db.CreateTaskMessage(c.Request.Context(), task.ID, userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_message", err)
		return
	}

	c.JSON(http.StatusCreated, message)
}

func (a *App) getAuthorizedTask(c *gin.Context) (models.Task, string, bool) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return models.Task{}, "", false
	}

	taskID := strings.TrimSpace(c.Param("task_id"))
	if _, err := uuid.Parse(taskID); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_task_id", map[string]string{"task_id": "task_id must be a UUID"})
		return models.Task{}, "", false
	}

	task, err := a.db.GetTask(c.Request.Context(), taskID)
	if err != nil {
		a.writeTaskDBError(c, "get_task", err)
		return models.Task{}, "", false
	}
	if !canAccessTask(task, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return models.Task{}, "", false
	}

	return task, userID, true
}

func (a *App) canUseDelegatedFromTask(c *gin.Context, delegatedFromTaskID *string, userID string) bool {
	if delegatedFromTaskID == nil {
		return true
	}
	if _, err := uuid.Parse(*delegatedFromTaskID); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_task", map[string]string{"delegated_from_task_id": "delegated_from_task_id must be a UUID"})
		return false
	}

	parent, err := a.db.GetTask(c.Request.Context(), *delegatedFromTaskID)
	if err != nil {
		a.writeTaskDBError(c, "get_delegated_from_task", err)
		return false
	}
	if !canAccessTask(parent, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return false
	}

	return true
}

func canAccessTask(task models.Task, userID string) bool {
	return task.CreatedByID == userID || task.AssignedToID == userID
}

func normalizeCreateTaskInput(input *models.CreateTask) {
	input.AssignedToID = strings.TrimSpace(input.AssignedToID)
	input.Title = strings.TrimSpace(input.Title)
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		input.Description = &description
	}
	if input.DelegatedFromTaskID != nil {
		delegatedFromTaskID := strings.TrimSpace(*input.DelegatedFromTaskID)
		input.DelegatedFromTaskID = &delegatedFromTaskID
	}
}

func normalizeUpdateTaskInput(input *models.UpdateTask) {
	if input.AssignedToID != nil {
		assignedToID := strings.TrimSpace(*input.AssignedToID)
		input.AssignedToID = &assignedToID
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		input.Title = &title
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		input.Description = &description
	}
	if input.DelegatedFromTaskID != nil {
		delegatedFromTaskID := strings.TrimSpace(*input.DelegatedFromTaskID)
		input.DelegatedFromTaskID = &delegatedFromTaskID
	}
}

func validateCreateTask(input models.CreateTask, creatorID string) map[string]string {
	details := map[string]string{}
	if input.AssignedToID == "" {
		details["assigned_to_id"] = "assigned_to_id is required"
	} else if input.AssignedToID == creatorID {
		details["assigned_to_id"] = "assigned_to_id cannot be the creator"
	}
	if input.Title == "" {
		details["title"] = "title is required"
	}
	if input.DelegatedFromTaskID != nil && *input.DelegatedFromTaskID == "" {
		details["delegated_from_task_id"] = "delegated_from_task_id cannot be empty"
	}
	return details
}

func validateUpdateTask(input models.UpdateTask, creatorID string) map[string]string {
	details := map[string]string{}
	if input.AssignedToID != nil {
		if *input.AssignedToID == "" {
			details["assigned_to_id"] = "assigned_to_id cannot be empty"
		} else if *input.AssignedToID == creatorID {
			details["assigned_to_id"] = "assigned_to_id cannot be the creator"
		}
	}
	if input.Title != nil && *input.Title == "" {
		details["title"] = "title cannot be empty"
	}
	if input.DelegatedFromTaskID != nil && *input.DelegatedFromTaskID == "" {
		details["delegated_from_task_id"] = "delegated_from_task_id cannot be empty"
	}
	return details
}

func expectationItems(tasks []models.Task) []models.TaskListItem {
	items := make([]models.TaskListItem, 0, len(tasks))
	for _, task := range tasks {
		item := taskListItem(task)
		item.AssignedBy = task.CreatedBy
		items = append(items, item)
	}
	return items
}

func todoItems(tasks []models.Task) []models.TaskListItem {
	items := make([]models.TaskListItem, 0, len(tasks))
	for _, task := range tasks {
		item := taskListItem(task)
		item.AssignedTo = task.AssignedTo
		items = append(items, item)
	}
	return items
}

func taskListItem(task models.Task) models.TaskListItem {
	return models.TaskListItem{
		ID:                  task.ID,
		Title:               task.Title,
		Description:         task.Description,
		Status:              task.Status,
		DueAt:               task.DueAt,
		DelegatedFromTaskID: task.DelegatedFromTaskID,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
		CompletedAt:         task.CompletedAt,
		CancelledAt:         task.CancelledAt,
	}
}

func (a *App) writeTaskDBError(c *gin.Context, handler string, err error) {
	switch {
	case errors.Is(err, sqldb.ErrDBNotFound):
		writeError(c, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, sqldb.ErrDBDuplicatedEntry):
		writeError(c, http.StatusConflict, "duplicate_resource", nil)
	case errors.Is(err, sqldb.ErrForeignKeyViolation):
		writeError(c, http.StatusBadRequest, "invalid_reference", nil)
	case errors.Is(err, sqldb.ErrCheckViolation), errors.Is(err, sqldb.ErrNotNullViolation):
		writeError(c, http.StatusBadRequest, "invalid_value", nil)
	default:
		a.toSentry(c, handler, "db", sentry.LevelError, err)
		writeError(c, http.StatusInternalServerError, "internal_error", nil)
	}
}
