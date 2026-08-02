package security

import "github.com/gin-gonic/gin"

type ServerHeaders struct {
	Version    string
	APIVersion string
	InstanceID string
}

func (h ServerHeaders) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Pixel-Telo-Server-Version", h.Version)
		c.Header("X-Pixel-Telo-API-Version", h.APIVersion)
		c.Header("X-Pixel-Telo-Instance-ID", h.InstanceID)
		c.Next()
	}
}
