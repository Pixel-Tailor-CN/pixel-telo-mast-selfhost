package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Mystery00/rollwriter"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

const (
	logDirectory = "logs"
	logFilename  = "mast.log"
	maxSizeMB    = int64((1<<63)-1) / rolling.MB
)

// ManagedLogger 管理统一 Logger 及其滚动文件 Writer 的生命周期。
type ManagedLogger struct {
	logger *slog.Logger
	writer *rolling.Writer
}

// New 创建同时写入文件和控制台的 slog Logger。
func New(dir string, cfg config.LogConfig) (*ManagedLogger, error) {
	return newManagedLogger(dir, cfg, os.Stderr)
}

func newManagedLogger(dir string, cfg config.LogConfig, consoleWriter io.Writer) (*ManagedLogger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	logsDir := filepath.Join(dir, logDirectory)
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	logPath := filepath.Join(logsDir, logFilename)
	if err := ensureLogFile(logPath); err != nil {
		return nil, err
	}
	maxSize, err := megabytesToBytes(cfg.Rotation.MaxSizeMB)
	if err != nil {
		return nil, fmt.Errorf("validate log rotation size: %w", err)
	}
	maxTotalSize, err := megabytesToBytes(cfg.Retention.MaxTotalSizeMB)
	if err != nil {
		return nil, fmt.Errorf("validate log retention size: %w", err)
	}
	consoleHandler := slog.NewTextHandler(consoleWriter, &slog.HandlerOptions{Level: slog.LevelWarn})
	console := slog.New(consoleHandler)
	writer, err := rolling.New(rolling.Config{
		Filename: logPath,
		Rotation: rolling.RotationConfig{
			MaxSize:   maxSize,
			Daily:     cfg.Rotation.Daily,
			LocalTime: cfg.Rotation.LocalTime,
		},
		Retention: rolling.RetentionConfig{
			MaxAge:       cfg.Retention.MaxAge.Std(),
			MaxBackups:   cfg.Retention.MaxBackups,
			MaxTotalSize: maxTotalSize,
		},
		Compression: rolling.CompressionConfig{Enabled: cfg.Rotation.Compress},
		OnError: func(err error) {
			console.Error("rolling log maintenance failed", "error", err)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create rolling log writer: %w", err)
	}
	reportedWriter := reportingWriter{writer: writer, report: func(err error) {
		console.Error("rolling log write failed", "error", err)
	}}
	fileOptions := &slog.HandlerOptions{Level: level}
	var fileHandler slog.Handler
	if cfg.Format == "text" {
		fileHandler = slog.NewTextHandler(reportedWriter, fileOptions)
	} else {
		fileHandler = slog.NewJSONHandler(reportedWriter, fileOptions)
	}
	handler := multiHandler{handlers: []slog.Handler{
		fileHandler,
		consoleHandler,
	}}
	return &ManagedLogger{logger: slog.New(handler), writer: writer}, nil
}

func megabytesToBytes(value int) (int64, error) {
	if value <= 0 || int64(value) > maxSizeMB {
		return 0, fmt.Errorf("megabyte value is out of range")
	}
	return int64(value) * rolling.MB, nil
}

func ensureLogFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close log file after validation: %w", err)
	}
	return nil
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", value)
	}
}

// Logger 返回受管的统一 Logger。
func (l *ManagedLogger) Logger() *slog.Logger { return l.logger }

// Close 关闭文件 Writer 并等待后台压缩和清理完成。
func (l *ManagedLogger) Close() error {
	if l == nil || l.writer == nil {
		return nil
	}
	if err := l.writer.Close(); err != nil {
		return fmt.Errorf("close rolling log writer: %w", err)
	}
	return nil
}

type multiHandler struct{ handlers []slog.Handler }

type reportingWriter struct {
	writer io.Writer
	report func(error)
}

func (w reportingWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err != nil && w.report != nil {
		w.report(err)
	}
	return n, err
}

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var first error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return multiHandler{handlers: handlers}
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return multiHandler{handlers: handlers}
}
