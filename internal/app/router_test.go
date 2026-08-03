package app

import (
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/gin-gonic/gin"
)

func TestNewRouterUsesReleaseMode(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(previousMode)

	if router := NewRouter(&httpapi.Handler{}); router == nil {
		t.Fatal("router is nil")
	}
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("Gin mode = %q", gin.Mode())
	}
}
