package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := flags.String("dir", ".", "data directory")
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
	if err := os.WriteFile(configPath, []byte(exampleConfig(tokenPath, filepath.Join(*dir, "runtime.db"))), 0o600); err != nil {
		return err
	}
	fmt.Printf("initialized config=%s token=%s\n", configPath, tokenPath)
	return nil
}

func exampleConfig(tokenPath, runtimePath string) string {
	return fmt.Sprintf("server:\n  listen: 127.0.0.1:8443\nauth:\n  token_file: %s\ntls:\n  mode: off\nstorage:\n  runtime_path: %s\nbaseline:\n  enabled: false\n  sync_on_start: false\n  check_interval: 24h\nquery:\n  timeout: 2s\n  max_concurrent: 4\nrate_limit:\n  requests_per_second: 1\n  burst: 5\nupstream:\n  provider_ids:\n    - sogou\nlog:\n  level: info\n  format: json\n", tokenPath, runtimePath)
}
