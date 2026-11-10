package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	jwtservice "github.com/light-group/light-ex-backend/internal/platform/security/jwt"
)

const (
	ContextUserID    = "user_id"
	ContextTokenType = "token_type"
)

func Authentication(jwt jwtservice.Service) gin.HandlerFunc {

	return func(c *gin.Context) {

		header := c.GetHeader("Authorization")

		if header == "" {

			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{
					"error": "missing authorization header",
				},
			)

			return
		}

		const prefix = "Bearer "

		if !strings.HasPrefix(header, prefix) {

			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{
					"error": "invalid authorization header",
				},
			)

			return
		}

		token := strings.TrimPrefix(header, prefix)

		claims, err := jwt.Validate(token)
		if err != nil {

			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{
					"error": "invalid token",
				},
			)

			return
		}

		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextTokenType, claims.TokenType)

		c.Next()
	}
}
