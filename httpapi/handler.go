package httpapi

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/phone"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var embeddedPhoneFinder = phone.NewFinder()

type Handler struct {
	Service      *service.Service
	Headers      security.ServerHeaders
	Token        []byte
	Limiter      *security.QueryLimiter
	BuildCommit  string
	Capabilities []string
	Logger       *slog.Logger
	// DisablePairing 为 true 时不注册 /p/:code。零值保持既有默认，继续注册配对页。
	DisablePairing bool
	pairingMu      sync.Mutex
	pairing        *pairingSession
}

func (h *Handler) Register(router *gin.Engine) {
	if h.Limiter == nil {
		h.Limiter = security.NewQueryLimiter(1, 5, 4)
	}
	router.GET("/", h.home)
	router.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	if !h.DisablePairing {
		router.GET("/p/:code", h.pairingPage)
	}
	authenticated := router.Group("/api", h.Headers.Middleware(), security.Bearer(h.Token))
	authenticated.GET("/selfhost/v1/info", h.info)
	authenticated.GET("/v2/sources", h.sources)
	queries := authenticated.Group("", h.Limiter.Middleware())
	queries.POST("/v2/query", h.queryV2)
}

func (h *Handler) info(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"service": "pixel-telo-mast-selfhost", "version": h.Headers.Version, "api_version": 2, "instance_id": h.Headers.InstanceID, "build_commit": h.BuildCommit, "capabilities": h.Capabilities})
}

func (h *Handler) sources(c *gin.Context) { c.JSON(http.StatusOK, h.Service.ListSources()) }

type queryV2Request struct {
	Number  string   `json:"number"`
	Sources []string `json:"sources"`
}

func (h *Handler) queryV2(c *gin.Context) {
	var request queryV2Request
	if err := c.ShouldBindJSON(&request); err != nil || !validPhone(request.Number) {
		h.writeError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := h.Service.LookupWithSources(c.Request.Context(), request.Number, request.Sources)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, makeQueryResponse(result, embeddedPhoneFinder.Find(result.Record.PhoneNumber)))
}

func (h *Handler) writeServiceError(c *gin.Context, err error) {
	status, code := errorStatus(err)
	requestID := ensureRequestID(c)
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("query failed", "status", status, "error_type", code, "request_id", requestID)
	c.JSON(status, gin.H{"error": code, "code": code, "request_id": requestID})
}

func (h *Handler) writeError(c *gin.Context, status int, code string) {
	requestID := ensureRequestID(c)
	c.JSON(status, gin.H{"error": code, "code": code, "request_id": requestID})
}

func ensureRequestID(c *gin.Context) string {
	requestID := c.Writer.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(requestID); err != nil {
		requestID = uuid.NewString()
	}
	c.Header("X-Request-ID", requestID)
	return requestID
}
