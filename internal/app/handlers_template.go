package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func (a *App) HandleCreateTaskTemplate(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	var input models.TaskTemplateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if details := validateTaskTemplateInput(input); len(details) > 0 {
		writeError(c, http.StatusBadRequest, "invalid_task_template", details)
		return
	}

	template, err := a.db.CreateTaskTemplate(c.Request.Context(), userID, input)
	if err != nil {
		a.writeTaskDBError(c, "create_task_template", err)
		return
	}

	c.JSON(http.StatusCreated, template)
}

func (a *App) HandleGetTaskTemplate(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	template, err := a.db.GetTaskTemplate(c.Request.Context(), c.Param("template_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_template", err)
		return
	}
	if !canManageTaskTemplate(template, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	c.JSON(http.StatusOK, template)
}

func (a *App) HandleUpdateTaskTemplate(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	template, err := a.db.GetTaskTemplate(c.Request.Context(), c.Param("template_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_template_for_update", err)
		return
	}
	if !canManageTaskTemplate(template, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	var input models.UpdateTaskTemplate
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_json", nil)
		return
	}
	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		writeError(c, http.StatusBadRequest, "invalid_task_template", map[string]string{"title": "title cannot be empty"})
		return
	}

	updated, err := a.db.UpdateTaskTemplate(c.Request.Context(), template.ID, input)
	if err != nil {
		a.writeTaskDBError(c, "update_task_template", err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (a *App) HandleArchiveTaskTemplate(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	template, err := a.db.GetTaskTemplate(c.Request.Context(), c.Param("template_id"))
	if err != nil {
		a.writeTaskDBError(c, "get_task_template_for_archive", err)
		return
	}
	if !canManageTaskTemplate(template, userID) {
		writeError(c, http.StatusForbidden, "forbidden", nil)
		return
	}

	if err := a.db.ArchiveTaskTemplate(c.Request.Context(), template.ID); err != nil {
		a.writeTaskDBError(c, "archive_task_template", err)
		return
	}

	c.Status(http.StatusNoContent)
}

func validateTaskTemplateInput(input models.TaskTemplateInput) map[string]string {
	details := map[string]string{}
	if strings.TrimSpace(input.Title) == "" {
		details["title"] = "title is required"
	}
	return details
}

func canManageTaskTemplate(template models.TaskTemplate, userID string) bool {
	return template.CreatedBy == userID
}
