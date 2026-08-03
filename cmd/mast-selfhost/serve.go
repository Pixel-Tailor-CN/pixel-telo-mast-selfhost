package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/app"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
)

var version = "0.1.0"
var commit = "unknown"

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	path := flags.String("config", "config.yaml", "config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, token, tlsConfig, err := prepareServe(*path)
	if err != nil {
		return err
	}
	application, err := app.Build(app.Options{Config: cfg, Token: token, Version: version, Commit: commit})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := application.Start(ctx, tlsConfig); err != nil {
		_ = application.Close(context.Background())
		return err
	}
	<-ctx.Done()
	return application.Close(context.Background())
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
