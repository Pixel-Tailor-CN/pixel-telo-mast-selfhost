package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxLogSizeMB = int64((1<<63)-1) / (1 << 20)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.Server.Listen) == "" || strings.TrimSpace(cfg.Storage.RuntimePath) == "" || strings.TrimSpace(cfg.Auth.TokenFile) == "" {
		return fmt.Errorf("server.listen, auth.token_file and storage.runtime_path are required")
	}
	if len(cfg.Upstream.ProviderIDs) == 0 {
		return fmt.Errorf("at least one upstream provider is required")
	}
	if cfg.Query.Timeout.Std() <= 0 || cfg.Query.Timeout.Std() > 10*time.Second || cfg.Query.MaxConcurrent <= 0 {
		return fmt.Errorf("query timeout or concurrency is invalid")
	}
	if cfg.RateLimit.RequestsPerSecond <= 0 || cfg.RateLimit.Burst <= 0 {
		return fmt.Errorf("rate limit is invalid")
	}
	switch cfg.TLS.Mode {
	case "auto":
		if err := validatePublicURL(cfg.TLS.PublicURL); err != nil {
			return err
		}
	case "files":
		if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
			return fmt.Errorf("tls certificate and key are required")
		}
	case "off":
		if !cfg.TLS.AllowInsecurePrivateNetwork && !isLoopbackListen(cfg.Server.Listen) {
			return fmt.Errorf("public cleartext must be rejected")
		}
	default:
		return fmt.Errorf("unsupported tls mode %q", cfg.TLS.Mode)
	}
	if cfg.Baseline.Enabled && cfg.Baseline.CheckInterval.Std() <= 0 {
		return fmt.Errorf("baseline check interval must be positive")
	}
	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log level %q", cfg.Log.Level)
	}
	switch cfg.Log.Format {
	case "json", "text":
	default:
		return fmt.Errorf("unsupported log format %q", cfg.Log.Format)
	}
	if cfg.Log.Rotation.MaxSizeMB <= 0 || int64(cfg.Log.Rotation.MaxSizeMB) > maxLogSizeMB {
		return fmt.Errorf("log rotation max size must be positive")
	}
	if cfg.Log.Retention.MaxAge.Std() <= 0 || cfg.Log.Retention.MaxBackups <= 0 || cfg.Log.Retention.MaxTotalSizeMB <= 0 || int64(cfg.Log.Retention.MaxTotalSizeMB) > maxLogSizeMB {
		return fmt.Errorf("log retention limits must be positive")
	}
	for id, provider := range cfg.Providers {
		if provider.MinInterval.Std() < 0 || provider.MaxConcurrent < 0 || provider.BreakerTimeout.Std() < 0 {
			return fmt.Errorf("provider %q configuration is invalid", id)
		}
	}
	return nil
}

func validatePublicURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("tls.public_url must be an HTTPS root URL")
	}
	return nil
}

func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = strings.TrimSpace(listen)
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func ReadToken(path string) ([]byte, error) {
	token, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth token: %w", err)
	}
	return normalizeAuthToken(token)
}

func normalizeAuthToken(raw []byte) ([]byte, error) {
	token := []byte(strings.TrimSpace(string(raw)))
	if len(token) < 32 {
		return nil, fmt.Errorf("auth token must contain at least 32 bytes")
	}
	return token, nil
}

func GenerateToken() ([]byte, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate auth token: %w", err)
	}
	return token, nil
}

func EnsureToken(path string) ([]byte, error) {
	token, err := ReadToken(path)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	randomToken, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	token = []byte(hex.EncodeToString(randomToken))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create auth token parent: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ReadToken(path)
		}
		return nil, fmt.Errorf("create auth token: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(token); err != nil {
		return nil, fmt.Errorf("write auth token: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close auth token: %w", err)
	}
	complete = true
	return token, nil
}

func EnsureParent(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config parent: %w", err)
	}
	return nil
}
