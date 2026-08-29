package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/app"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

var version = "1.0.0"
var commit = "unknown"

func main() {
	if err := run(context.Background(), os.Getenv); err != nil {
		slog.Error("vercel server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) (err error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(previous)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadVercel(getenv)
	if err != nil {
		return fmt.Errorf("load vercel config: %w", err)
	}
	application, err := app.BuildVercel(ctx, cfg, logger, version, commit)
	if err != nil {
		return err
	}
	defer func() {
		err = closeApplication(err, application.Close)
	}()

	server, err := newServer(getenv("PORT"), application.Handler)
	if err != nil {
		return fmt.Errorf("listen vercel server: %w", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting vercel server", "addr", server.Addr, "version", version, "commit", commit)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case listenErr := <-serveErr:
		if errors.Is(listenErr, http.ErrServerClosed) {
			return nil
		}
		if listenErr != nil {
			return fmt.Errorf("listen vercel server: %w", listenErr)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			return fmt.Errorf("shutdown vercel server: %w", shutdownErr)
		}
		listenErr := <-serveErr
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			return fmt.Errorf("listen vercel server: %w", listenErr)
		}
		slog.Info("vercel server stopped", "addr", server.Addr)
		return nil
	}
}

func closeApplication(runErr error, close func() error) error {
	if close == nil {
		return runErr
	}
	return errors.Join(runErr, close())
}

func newServer(port string, handler http.Handler) (*http.Server, error) {
	port = strings.TrimSpace(port)
	if port == "" {
		return nil, errors.New("PORT is required")
	}
	if handler == nil {
		return nil, errors.New("handler is required")
	}
	return &http.Server{Addr: ":" + port, Handler: handler}, nil
}
