package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
)

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := flags.String("dir", ".", "data directory")
	publicURL := flags.String("public-url", "", "public HTTPS URL for auto TLS")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		return err
	}
	token, err := config.GenerateToken()
	if err != nil {
		return err
	}
	tokenPath := filepath.Join(*dir, "token")
	if err := os.WriteFile(tokenPath, []byte(hex.EncodeToString(token)), 0o600); err != nil {
		return err
	}
	configPath := filepath.Join(*dir, "config.yaml")
	runtimePath := filepath.Join(*dir, "runtime.db")
	runtimeRepo, err := runtime.Open(runtimePath)
	if err != nil {
		return err
	}
	instanceID, err := runtimeRepo.EnsureInstanceID(context.Background())
	_ = runtimeRepo.Close()
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(exampleConfig(tokenPath, runtimePath, *publicURL)), 0o600); err != nil {
		return err
	}
	if *publicURL != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if _, err := security.PrepareTLS(cfg); err != nil {
			return err
		}
	}
	fmt.Printf("initialized config=%s token=%s instance_id=%s\n", configPath, tokenPath, instanceID)
	return nil
}

func exampleConfig(tokenPath, runtimePath, publicURL string) string {
	tlsMode := "off"
	if publicURL != "" {
		tlsMode = "auto"
	}
	return fmt.Sprintf("server:\n  listen: 127.0.0.1:8443\nauth:\n  token_file: '%s'\ntls:\n  mode: %s\n  public_url: '%s'\nstorage:\n  runtime_path: '%s'\nbaseline:\n  enabled: false\n  sync_on_start: false\n  check_interval: 24h\nquery:\n  timeout: 2s\n  max_concurrent: 4\nrate_limit:\n  requests_per_second: 1\n  burst: 5\nupstream:\n  provider_ids:\n    - sogou\nlog:\n  level: info\n  format: json\n", tokenPath, tlsMode, publicURL, runtimePath)
}
