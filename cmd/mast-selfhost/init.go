package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

//go:embed config.example.yaml
var exampleConfigTemplate string

type exampleConfigData struct {
	TokenPath                   string
	RuntimePath                 string
	Listen                      string
	TLSMode                     string
	PublicURL                   string
	AllowInsecurePrivateNetwork bool
	ProviderIDs                 string
	SyncOnStart                 bool
}

type initOptions struct {
	dir           string
	listen        string
	listenSet     bool
	tlsMode       string
	publicURL     string
	allowInsecure bool
	ifMissing     bool
	providerIDs   []string
	syncOnStart   bool
}

func runInit(args []string) error {
	command := newInitCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newInitCommand() *cobra.Command {
	options := initOptions{syncOnStart: true}
	command := &cobra.Command{
		Use:   "init",
		Short: "生成初始配置文件",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options.listenSet = command.Flags().Changed("listen")
			if options.ifMissing {
				if _, err := os.Stat(filepath.Join(options.dir, "config.yaml")); err == nil {
					return nil
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return initDataDirectory(options)
		},
	}
	command.Flags().StringVar(&options.dir, "dir", ".", "数据目录")
	command.Flags().StringVar(&options.listen, "listen", "0.0.0.0:8443", "监听地址")
	command.Flags().StringVar(&options.tlsMode, "tls-mode", "auto", "TLS 模式：auto、files 或 off")
	command.Flags().StringVar(&options.publicURL, "public-url", "", "客户端访问的 HTTPS 根 URL，auto 模式写入证书 SAN")
	command.Flags().BoolVar(&options.allowInsecure, "allow-insecure-private-network", false, "允许私有网络使用裸 HTTP")
	command.Flags().BoolVar(&options.ifMissing, "if-missing", false, "配置已存在时直接成功")
	command.Flags().StringSliceVar(&options.providerIDs, "provider-id", nil, "显式启用的 Provider，可重复传入")
	command.Flags().BoolVar(&options.syncOnStart, "sync-on-start", true, "启动时是否同步 baseline")
	return command
}

func initDataDirectory(options initOptions) error {
	if err := os.MkdirAll(options.dir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	configPath := filepath.Join(options.dir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("create config: file exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("create config: %w", err)
	}
	data, listen, publicURL, err := renderExampleConfig(options)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(configPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	complete = true
	fmt.Printf("initialized config=%s listen=%s public_url=%s\n", configPath, listen, publicURL)
	if loopbackPublicURL(publicURL) {
		fmt.Fprintln(os.Stderr, "当前 public_url 只能本机访问，手机连不上。请在终端重新运行 init 并选择局域网地址，或加上 --listen 0.0.0.0:8443 --public-url https://你的IP:8443")
	}
	return nil
}

func renderExampleConfig(options initOptions) ([]byte, string, string, error) {
	absDir, err := filepath.Abs(options.dir)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve data directory: %w", err)
	}
	tlsMode, publicURL, listen, err := resolveInitTLS(options)
	if err != nil {
		return nil, "", "", err
	}
	tmpl, err := template.New("config.example.yaml").Parse(exampleConfigTemplate)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse embedded config template: %w", err)
	}
	values := exampleConfigData{
		TokenPath:                   strconv.Quote(filepath.Join(absDir, "token")),
		RuntimePath:                 strconv.Quote(filepath.Join(absDir, "runtime.db")),
		Listen:                      strconv.Quote(listen),
		TLSMode:                     strconv.Quote(tlsMode),
		PublicURL:                   strconv.Quote(publicURL),
		AllowInsecurePrivateNetwork: options.allowInsecure,
		ProviderIDs:                 providerIDsYAML(options.providerIDs),
		SyncOnStart:                 options.syncOnStart,
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, values); err != nil {
		return nil, "", "", fmt.Errorf("render embedded config template: %w", err)
	}
	return output.Bytes(), listen, publicURL, nil
}

func resolveInitTLS(options initOptions) (string, string, string, error) {
	tlsMode := strings.TrimSpace(options.tlsMode)
	if tlsMode == "" {
		tlsMode = "auto"
	}
	switch tlsMode {
	case "auto", "files", "off":
	default:
		return "", "", "", fmt.Errorf("unsupported tls mode %q", tlsMode)
	}
	listen := strings.TrimSpace(options.listen)
	publicURL := strings.TrimSpace(options.publicURL)
	if publicURL == "" && tlsMode == "auto" {
		inferred, ok := inferredPublicURL(listen)
		if ok && (!loopbackListenHost(listen) || options.listenSet || !initInteractive()) {
			publicURL = inferred
		} else {
			selected, err := selectPublicURL(listen)
			if err != nil {
				return "", "", "", err
			}
			publicURL = selected
		}
	}
	if publicURL != "" {
		if err := validateInitPublicURL(publicURL); err != nil {
			return "", "", "", err
		}
	}
	listen = widenListenIfNeeded(listen, publicURL)
	return tlsMode, publicURL, listen, nil
}

func inferredPublicURL(listen string) (string, bool) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return "", false
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return "", false
	}
	return "https://" + net.JoinHostPort(host, port), true
}

func widenListenIfNeeded(listen, publicURL string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || !loopbackHost(host) {
		return listen
	}
	parsed, err := url.Parse(publicURL)
	if err != nil || loopbackHost(parsed.Hostname()) {
		return listen
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func loopbackPublicURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return loopbackHost(parsed.Hostname())
}

func loopbackListenHost(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return false
	}
	return loopbackHost(host)
}

func loopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateInitPublicURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("tls.public_url must be an HTTPS root URL")
	}
	return nil
}

func providerIDsYAML(providerIDs []string) string {
	if len(providerIDs) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		quoted = append(quoted, strconv.Quote(strings.TrimSpace(providerID)))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
