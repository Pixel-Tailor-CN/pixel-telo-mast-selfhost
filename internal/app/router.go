package app

import (
	"log/slog"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/logging"
	"github.com/gin-gonic/gin"
)

func NewRouter(handler *httpapi.Handler, loggers ...*slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	router := gin.New()
	router.Use(logging.Access(logger), logging.Recovery(logger))
	handler.Register(router)
	return router
}
