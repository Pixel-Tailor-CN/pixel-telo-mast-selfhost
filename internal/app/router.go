package app

import (
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/gin-gonic/gin"
)

func NewRouter(handler *httpapi.Handler) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	handler.Register(router)
	return router
}
