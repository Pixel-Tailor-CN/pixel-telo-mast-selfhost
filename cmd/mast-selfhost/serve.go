package main

import (
	"context"
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
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	token, err := config.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return err
	}
	application, err := app.Build(app.Options{Config: cfg, Token: token, Version: version, Commit: commit, InstanceID: "local"})
	if err != nil {
		return err
	}
	tlsConfig, err := security.PrepareTLS(cfg)
	if err != nil {
		_ = application.Close(context.Background())
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

func versionString() string { return fmt.Sprintf("%s commit=%s api=2", version, commit) }
