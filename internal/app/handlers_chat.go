package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/sdk/middleware"
	"github.com/nourabuild/relays-api/internal/sdk/models"
	"github.com/nourabuild/relays-api/internal/services/jwt"
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

	if a.ws != nil {
		a.ws.BroadcastTaskMessage(task.ID, message)
	}

	c.JSON(http.StatusCreated, message)
}

func (a *App) HandleTaskChatWebSocket(c *gin.Context) {
	userID, ok := a.authenticateWebSocket(c)
	if !ok {
		return
	}
	c.Set(middleware.UserIDKey, userID)

	task, _, ok := a.getAuthorizedTask(c)
	if !ok {
		return
	}

	if a.ws == nil {
		writeError(c, http.StatusInternalServerError, "websocket_unavailable", nil)
		return
	}

	_ = a.ws.ServeTask(c.Writer, c.Request, task.ID)
}

func (a *App) authenticateWebSocket(c *gin.Context) (string, bool) {
	if a.jwt == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return "", false
	}

	token, ok := websocketToken(c)
	if !ok {
		return "", false
	}

	claims, err := a.jwt.ParseAccessToken(c.Request.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrExpiredToken):
			writeError(c, http.StatusUnauthorized, "expired_token", nil)
		case errors.Is(err, jwt.ErrTokenNotFound):
			writeError(c, http.StatusUnauthorized, "missing_authorization_header", nil)
		case errors.Is(err, jwt.ErrInvalidToken), errors.Is(err, jwt.ErrInvalidClaims), errors.Is(err, jwt.ErrTokenNotYetValid):
			writeError(c, http.StatusUnauthorized, "invalid_token", nil)
		default:
			writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		}
		return "", false
	}

	if claims.Subject == "" {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return "", false
	}

	return claims.Subject, true
}

func websocketToken(c *gin.Context) (string, bool) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
			writeError(c, http.StatusUnauthorized, "invalid_authorization_header", nil)
			return "", false
		}
		return strings.TrimSpace(parts[1]), true
	}

	if token := strings.TrimSpace(c.Query("access_token")); token != "" {
		return token, true
	}
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token, true
	}

	writeError(c, http.StatusUnauthorized, "missing_authorization_header", nil)
	return "", false
}
