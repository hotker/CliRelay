package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
)

// RequestBodyCleanupMiddleware releases reusable request-body storage after the
// request has finished logging and handler processing.
func RequestBodyCleanupMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		bodyutil.CleanupRequestBody(c)
	}
}
