package httpapi

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const projectRepositoryURL = "https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost"

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>Pixel Telo Mast Self-host</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    * { box-sizing: border-box; }
    body { min-height: 100vh; margin: 0; display: grid; place-items: center; padding: 24px; color: #e8edf5; background: radial-gradient(circle at top, #183148 0, #0b1119 48%, #070a0f 100%); }
    main { width: min(720px, 100%); padding: clamp(28px, 6vw, 56px); border: 1px solid #26384c; border-radius: 24px; background: rgba(11, 17, 25, .88); box-shadow: 0 24px 80px rgba(0, 0, 0, .35); }
    .status { display: inline-flex; align-items: center; gap: 10px; margin-bottom: 24px; padding: 8px 13px; border: 1px solid #236847; border-radius: 999px; color: #8ce9b8; background: #0e2b20; font-size: 14px; font-weight: 700; }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: #45da8a; box-shadow: 0 0 14px #45da8a; }
    h1 { margin: 0 0 14px; font-size: clamp(30px, 7vw, 52px); line-height: 1.08; letter-spacing: -.035em; }
    .lead { margin: 0; color: #aebdce; font-size: 17px; line-height: 1.75; }
    dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin: 34px 0; }
    dl div { padding: 16px; border: 1px solid #223143; border-radius: 14px; background: #0f1823; }
    dt { margin-bottom: 7px; color: #8193a7; font-size: 12px; text-transform: uppercase; letter-spacing: .08em; }
    dd { margin: 0; font-weight: 700; overflow-wrap: anywhere; }
    .sources { grid-column: 1 / -1; }
    nav { display: flex; flex-wrap: wrap; gap: 12px; }
    a { color: #80c9ff; text-decoration: none; }
    a:hover { text-decoration: underline; }
    nav a { padding: 10px 14px; border: 1px solid #2b4560; border-radius: 10px; background: #101e2c; }
    footer { margin-top: 28px; color: #68798c; font-size: 13px; }
    @media (max-width: 520px) { dl { grid-template-columns: 1fr; } .sources { grid-column: auto; } }
  </style>
</head>
<body>
  <main>
    <div class="status"><span class="dot" aria-hidden="true"></span>运行正常</div>
    <h1>Pixel Telo Mast Self-host</h1>
    <p class="lead">可独立部署的实时号码标记查询服务。此实例仅通过已启用的本地 Provider 完成查询，不会将实时查询自动转发至官方 Mast。</p>
    <dl>
      <div><dt>服务版本</dt><dd>{{.Version}}</dd></div>
      <div><dt>API 版本</dt><dd>{{.APIVersion}}</dd></div>
      <div class="sources"><dt>已启用数据源</dt><dd>{{.Sources}}</dd></div>
    </dl>
    <nav aria-label="相关链接">
      <a href="/api/health">健康检查</a>
      <a href="{{.RepositoryURL}}" rel="noreferrer">项目仓库</a>
    </nav>
    <footer>页面可访问表示 Self-host HTTP 服务正在运行；数据源名称不代表上游当前一定可用。</footer>
  </main>
</body>
</html>`))

type homePageData struct {
	Version       string
	APIVersion    string
	Sources       string
	RepositoryURL string
}

func (h *Handler) home(c *gin.Context) {
	sources := "未配置"
	if h.Service != nil {
		configured := h.Service.ListSources().DefaultSources
		if len(configured) > 0 {
			sources = strings.Join(configured, "、")
		}
	}

	data := homePageData{
		Version:       displayValue(h.Headers.Version),
		APIVersion:    displayValue(h.Headers.APIVersion),
		Sources:       sources,
		RepositoryURL: projectRepositoryURL,
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
