package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/app"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/logging"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/spf13/cobra"
)

var version = "1.0.0"
var commit = "unknown"

func runServe(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runServeContext(ctx, args)
}

func runServeContext(ctx context.Context, args []string) error {
	command := newServeCommand()
	command.SetArgs(args)
	command.SetContext(ctx)
	return command.Execute()
}

func newServeCommand() *cobra.Command {
	var dir string
	command := &cobra.Command{
		Use:   "serve",
		Short: "启动自建查询服务",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return serveContext(command.Context(), dir)
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "数据目录")
	return command
}

func serveContext(ctx context.Context, dir string) (resultErr error) {
	path := filepath.Join(dir, "config.yaml")
	cfg, token, tlsConfig, err := prepareServe(path)
	if err != nil {
		return err
	}
	managedLogger, err := logging.New(dir, cfg.Log)
	if err != nil {
		return err
	}
	previousLogger := slog.Default()
	slog.SetDefault(managedLogger.Logger())
	defer func() {
		slog.SetDefault(previousLogger)
		resultErr = errors.Join(resultErr, managedLogger.Close())
	}()
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
	managedLogger.ConsoleInfo("self-host server started",
		"listen", cfg.Server.Listen,
		"tls_mode", cfg.TLS.Mode,
		"log_file", managedLogger.LogPath(),
		"log_hint", "view this file for detailed logs",
	)
	if pairingURL := application.PairingPageURL(); pairingURL != "" {
		fmt.Fprintf(os.Stderr, "请在 5 分钟内用浏览器打开配对页面（含二维码和复制按钮）：\n%s\n", pairingURL)
		managedLogger.ConsoleInfo("pairing setup page ready", "expires", "5m")
	}
	for _, accessURL := range candidateAccessURLs(cfg.Server.Listen, cfg.TLS.PublicURL) {
		message := "candidate access url: " + accessURL
		slog.Info(message)
		managedLogger.ConsoleInfo(message)
	}
	<-ctx.Done()
	slog.Info("shutdown signal received")
	if err := application.Close(context.Background()); err != nil {
		return err
	}
	slog.Info("self-host server stopped")
	return nil
}

func serveConfigPath(args []string) (string, error) {
	command := newServeCommand()
	if err := command.ParseFlags(args); err != nil {
		return "", err
	}
	if err := cobra.NoArgs(command, command.Flags().Args()); err != nil {
		return "", err
	}
	dir, err := command.Flags().GetString("dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
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
