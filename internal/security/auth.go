package security

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Bearer(token []byte) gin.HandlerFunc {
	want := append([]byte(nil), token...)
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		prefix := "Bearer "
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) || subtle.ConstantTimeCompare([]byte(header[len(prefix):]), want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "unauthorized"})
			return
		}
		c.Next()
	}
}
