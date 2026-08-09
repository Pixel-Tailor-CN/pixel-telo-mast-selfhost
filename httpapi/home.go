package httpapi

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	projectRepositoryURL = "https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost"
	deploymentGuideURL   = projectRepositoryURL + "/blob/main/DEPLOY.md"
)

//go:embed assets/home.html
var homeHTML string

var homeTemplate = template.Must(template.New("home").Parse(homeHTML))

type homePageData struct {
	Version       string
	APIVersion    string
	Sources       []string
	RepositoryURL string
	DeploymentURL string
}

func (h *Handler) home(c *gin.Context) {
	var sources []string
	if h.Service != nil {
		sources = h.Service.ListSources().DefaultSources
	}

	data := homePageData{
		Version:       displayVersion(h.Headers.Version),
		APIVersion:    displayVersion(h.Headers.APIVersion),
		Sources:       sources,
		RepositoryURL: projectRepositoryURL,
		DeploymentURL: deploymentGuideURL,
	}
	var page bytes.Buffer
	if err := homeTemplate.Execute(&page, data); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Data(http.StatusOK, "text/html; charset=utf-8", page.Bytes())
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func displayVersion(value string) string {
	value = displayValue(value)
	if value == "未知" || strings.HasPrefix(strings.ToLower(value), "v") {
		return value
	}
	return "v" + value
}
