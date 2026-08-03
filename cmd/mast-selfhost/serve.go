package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/app"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
)

var version = "0.1.0"
var commit = "unknown"

func runServe(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runServeContext(ctx, args)
}

func runServeContext(ctx context.Context, args []string) error {
	path, err := serveConfigPath(args)
	if err != nil {
		return err
	}
	cfg, token, tlsConfig, err := prepareServe(path)
	if err != nil {
		return err
	}
	slog.Info("configuration loaded", "config_path", path, "listen", cfg.Server.Listen, "tls_mode", cfg.TLS.Mode)
	application, err := app.Build(app.Options{Config: cfg, Token: token, Version: version, Commit: commit})
	if err != nil {
		return err
	}
	slog.Info("starting self-host server", "listen", cfg.Server.Listen, "tls_mode", cfg.TLS.Mode, "version", version, "commit", commit)
	if err := application.Start(ctx, tlsConfig); err != nil {
		_ = application.Close(context.Background())
		return err
	}
	slog.Info("self-host server started", "listen", cfg.Server.Listen, "tls_mode", cfg.TLS.Mode)
	<-ctx.Done()
	slog.Info("shutdown signal received")
	if err := application.Close(context.Background()); err != nil {
		return err
	}
	slog.Info("self-host server stopped")
	return nil
}

func serveConfigPath(args []string) (string, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := flags.String("dir", ".", "data directory")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("unexpected serve arguments: %v", flags.Args())
	}
	return filepath.Join(*dir, "config.yaml"), nil
}

func prepareServe(path string) (*config.Config, []byte, *tls.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, fmt.Errorf("config file does not exist: %q", path)
		}
		return nil, nil, nil, fmt.Errorf("inspect config file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, nil, fmt.Errorf("config path is not a regular file: %q", path)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate config file %q: %w", path, err)
	}
	token, err := config.EnsureToken(cfg.Auth.TokenFile)
	if err != nil {
		return nil, nil, nil, err
	}
	tlsConfig, err := security.PrepareTLS(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, token, tlsConfig, nil
}

func versionString() string { return fmt.Sprintf("%s commit=%s api=2", version, commit) }
