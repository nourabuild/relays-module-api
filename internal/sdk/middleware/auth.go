package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nourabuild/relays-api/internal/services/jwt"
)

const (
	UserIDKey = "user_id"
)

// Authenticate validates the Authorization header and attaches user context.
func Authenticate(jwtService *jwt.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing_authorization_header"})
			c.Abort()
			return
		}

		// Expect "Bearer <token>" format.
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_authorization_header"})
			c.Abort()
			return
		}

		claims, err := jwtService.ParseAccessToken(c.Request.Context(), parts[1])
		if err != nil {
			switch {
			case errors.Is(err, jwt.ErrExpiredToken):
				c.JSON(http.StatusUnauthorized, gin.H{"error": "expired_token"})
			case errors.Is(err, jwt.ErrInvalidToken):
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			default:
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			}
			c.Abort()
			return
		}

		if claims.Subject == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		c.Set(UserIDKey, claims.Subject)
		c.Next()
	}
}
