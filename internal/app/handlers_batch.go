package app

import (
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
	if details := validateCreateTaskBatch(input); len(details) > 0 {
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

func (a *App) HandleListMyTaskBatchProgress(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	progresses, err := a.db.ListTaskBatchProgressByCreator(c.Request.Context(), userID, true)
	if err != nil {
		a.writeTaskDBError(c, "list_my_task_batch_progress", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"batches": progresses})
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
	if !canManageTaskBatch(progress.CreatedBy, userID, currentUserIsAdmin(c)) {
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
	if !canManageTaskBatch(batch.CreatedBy, userID, currentUserIsAdmin(c)) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return models.TaskBatch{}, false
	}

	return batch, true
}

func validateCreateTaskBatch(input models.CreateTaskBatch) map[string]string {
	details := map[string]string{}

	if input.TemplateID == nil && input.Template == nil {
		details["template"] = "template or template_id is required"
	}
	if input.TemplateID != nil && input.Template != nil {
		details["template"] = "provide either template or template_id, not both"
	}
	if input.Template != nil {
		for key, value := range validateTaskTemplateInput(*input.Template) {
			details["template."+key] = value
		}
	}
	if input.TemplateID != nil && strings.TrimSpace(*input.TemplateID) == "" {
		details["template_id"] = "template_id cannot be empty"
	}
	if input.AssignmentMode != nil && !models.IsValidAssignmentMode(*input.AssignmentMode) {
		details["assignment_mode"] = "assignment_mode must be same_work or customized_work"
	}
	if len(input.Assignments) == 0 {
		details["assignments"] = "at least one assignment is required"
	}

	assignmentKeys := map[string]bool{}
	for index, assignment := range input.Assignments {
		prefix := "assignments[" + strconv.Itoa(index) + "]."
		if strings.TrimSpace(assignment.AssigneeID) == "" {
			details[prefix+"assignee_id"] = "assignee_id is required"
		}
		if assignment.AssignmentKey != nil && strings.TrimSpace(*assignment.AssignmentKey) == "" {
			details[prefix+"assignment_key"] = "assignment_key cannot be empty"
		} else if assignment.AssignmentKey != nil {
			if assignmentKeys[*assignment.AssignmentKey] {
				details[prefix+"assignment_key"] = "assignment_key must be unique within the batch"
			}
			assignmentKeys[*assignment.AssignmentKey] = true
		}
		if assignment.Overrides != nil && assignment.Overrides.Title != nil && strings.TrimSpace(*assignment.Overrides.Title) == "" {
			details[prefix+"overrides.title"] = "override title cannot be empty"
		}
	}
	for index, dependency := range input.Dependencies {
		prefix := "dependencies[" + strconv.Itoa(index) + "]."
		if strings.TrimSpace(dependency.AssignmentKey) == "" {
			details[prefix+"assignment_key"] = "assignment_key is required"
		} else if !assignmentKeys[dependency.AssignmentKey] {
			details[prefix+"assignment_key"] = "assignment_key must reference an assignment in this batch"
		}
		if strings.TrimSpace(dependency.DependsOnAssignmentKey) == "" {
			details[prefix+"depends_on_assignment_key"] = "depends_on_assignment_key is required"
		} else if !assignmentKeys[dependency.DependsOnAssignmentKey] {
			details[prefix+"depends_on_assignment_key"] = "depends_on_assignment_key must reference an assignment in this batch"
		}
		if dependency.AssignmentKey == dependency.DependsOnAssignmentKey {
			details[prefix+"depends_on_assignment_key"] = "task cannot depend on itself"
		}
		if dependency.DependencyType != nil && !models.IsValidTaskDependencyType(*dependency.DependencyType) {
			details[prefix+"dependency_type"] = "dependency_type must be blocks_start or blocks_completion"
		}
	}

	return details
}

func canManageTaskBatch(createdBy, userID string, isAdmin bool) bool {
	return isAdmin || createdBy == userID
}
