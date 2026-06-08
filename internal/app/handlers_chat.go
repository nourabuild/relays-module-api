package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/models"
)

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
