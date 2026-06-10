package app

import (
	"encoding/json"
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

// authHTTPClient bounds calls to the auth service so a hung upstream cannot
// hold handler goroutines open indefinitely.
var authHTTPClient = &http.Client{Timeout: 10 * time.Second}

func (a *App) HandleMe(c *gin.Context) {
	userID, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	user, err := a.db.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		if !errors.Is(err, sqldb.ErrDBNotFound) {
			a.toSentry(c, "me", "db", sentry.LevelError, err)
			writeError(c, http.StatusInternalServerError, "internal_verify_user_error", nil)
			return
		}

		// User not found locally — fetch from auth service and create
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, a.cfg.Auth.MeURL, nil)
		if err != nil {
			a.toSentry(c, "me", "http", sentry.LevelError, err)
			writeError(c, http.StatusInternalServerError, "internal_auth_request_error", nil)
			return
		}
		req.Header.Set("Authorization", c.GetHeader("Authorization"))

		resp, err := authHTTPClient.Do(req)
		if err != nil {
			a.toSentry(c, "me", "http", sentry.LevelError, err)
			writeError(c, http.StatusInternalServerError, "internal_auth_request_error", nil)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			writeError(c, http.StatusUnauthorized, "auth_service_error", nil)
			return
		}

		var authUser models.User
		if err := json.NewDecoder(resp.Body).Decode(&authUser); err != nil {
			a.toSentry(c, "me", "decode", sentry.LevelError, err)
			writeError(c, http.StatusInternalServerError, "internal_auth_decode_error", nil)
			return
		}

		user, err = a.db.CreateUser(c.Request.Context(), models.NewUser{
			ID:      authUser.ID,
			Name:    authUser.Name,
			Account: authUser.Account,
			Email:   authUser.Email,
		})
		if err != nil {
			a.toSentry(c, "me", "db", sentry.LevelError, err)
			writeError(c, http.StatusInternalServerError, "internal_create_user_error", nil)
			return
		}
		// Automatically add user settings, I guess... but not related here
	}

	c.JSON(http.StatusOK, user)
}

func (a *App) HandleSearchUsers(c *gin.Context) {
	// Verify authentication
	_, err := middleware.GetClaims(c)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	// Get the search query parameter
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusOK, []models.PublicUser{})
		return
	}

	// Search users in database
	users, err := a.db.SearchUsers(c.Request.Context(), query)
	if err != nil {
		a.toSentry(c, "search_users", "db", sentry.LevelError, err)
		writeError(c, http.StatusInternalServerError, "internal_search_error", nil)
		return
	}

	// Other users' profiles are public-shape only: no email/phone/DOB
	profiles := make([]models.PublicUser, 0, len(users))
	for _, user := range users {
		profiles = append(profiles, models.PublicProfile(user))
	}

	c.JSON(http.StatusOK, profiles)
}

func (a *App) HandleAccountLookup(c *gin.Context) {
	if _, err := middleware.GetClaims(c); err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	account := strings.TrimSpace(c.Param("account"))
	if account == "" {
		writeError(c, http.StatusBadRequest, "invalid_account", map[string]string{"account": "account is required"})
		return
	}

	user, err := a.db.GetUserByAccount(c.Request.Context(), account)
	if err != nil {
		if errors.Is(err, sqldb.ErrDBNotFound) {
			writeError(c, http.StatusNotFound, "not_found", nil)
			return
		}
		a.toSentry(c, "account_lookup", "db", sentry.LevelError, err)
		writeError(c, http.StatusInternalServerError, "internal_lookup_error", nil)
		return
	}

	c.JSON(http.StatusOK, models.PublicProfile(user))
}
