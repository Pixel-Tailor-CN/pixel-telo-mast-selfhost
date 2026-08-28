package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
	"github.com/spf13/cobra"
)

type pairingInfo struct {
	URL        string
	Token      string
	InstanceID string
	SPKIPin    string
}

func runPairing(args []string) error {
	command := newPairingCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newPairingCommand() *cobra.Command {
	var dir string
	command := &cobra.Command{
		Use:     "pairing",
		Aliases: []string{"pair"},
		Short:   "输出客户端配对信息",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return printPairingInfo(filepath.Join(dir, "config.yaml"))
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "数据目录")
	return command
}

func printPairingInfo(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	token, err := config.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return fmt.Errorf("pairing requires a successful serve so the token exists: %w", err)
	}
	info, err := buildPairingInfo(cfg, token)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "配对信息包含密钥，不要发到群里或截图外传。")
	if loopbackHost(mustHostname(info.URL)) {
		fmt.Fprintln(os.Stderr, "当前地址是本机回环地址，手机通常连不上。")
	}
	fmt.Printf("地址: %s\n", info.URL)
	fmt.Printf("令牌: %s\n", info.Token)
	fmt.Printf("证书指纹: %s\n", info.SPKIPin)
	payload, err := json.Marshal(struct {
		URL     string `json:"url"`
		Token   string `json:"token"`
		SPKIPin string `json:"spki_pin"`
	}{URL: info.URL, Token: info.Token, SPKIPin: info.SPKIPin})
	if err != nil {
		return err
	}
	fmt.Printf("\n可粘贴的 JSON：\n%s\n", payload)
	fmt.Fprintln(os.Stderr, "若服务刚启动，也可在 5 分钟内打开启动日志里的配对页面，用复制按钮或二维码发到手机。")
	return nil
}

func buildPairingInfo(cfg *config.Config, token []byte) (pairingInfo, error) {
	instanceID, err := instanceID(cfg)
	if err != nil {
		return pairingInfo{}, err
	}
	publicURL := strings.TrimRight(cfg.TLS.PublicURL, "/")
	if publicURL == "" {
		if listenIsUnspecified(cfg.Server.Listen) {
			return pairingInfo{}, fmt.Errorf("tls.public_url is required because listen %q is not a concrete host", cfg.Server.Listen)
		}
		publicURL = "https://" + cfg.Server.Listen
	}
	if err := validatePairingOutputURL(publicURL); err != nil {
		return pairingInfo{}, err
	}
	pin := ""
	if cfg.TLS.Mode != "off" {
		certFile, _ := cfg.TLSFiles()
		pin, err = security.CertificateSPKI(certFile)
		if err != nil {
			return pairingInfo{}, fmt.Errorf("pairing requires a successful serve so the TLS certificate exists: %w", err)
		}
	}
	return pairingInfo{URL: publicURL, Token: string(token), InstanceID: instanceID, SPKIPin: pin}, nil
}

func validatePairingOutputURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("pairing url must be an HTTPS root URL")
	}
	host := parsed.Hostname()
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		return fmt.Errorf("pairing url host %q is not reachable from a phone", host)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return fmt.Errorf("pairing url host %q is not reachable from a phone", host)
	}
	return nil
}

func mustHostname(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func instanceID(cfg *config.Config) (string, error) {
	repo, err := runtime.Open(cfg.Storage.RuntimePath)
	if err != nil {
		return "", err
	}
	value, ensureErr := repo.EnsureInstanceID(context.Background())
	closeErr := repo.Close()
	if ensureErr != nil {
		return "", ensureErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("close runtime database: %w", closeErr)
	}
	return value, nil
}
