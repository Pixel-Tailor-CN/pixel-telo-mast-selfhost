package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

func testLogConfig() config.LogConfig {
	return config.LogConfig{
		Level:  "info",
		Format: "json",
		Rotation: config.LogRotationConfig{
			MaxSizeMB: 1,
			Daily:     true,
			LocalTime: true,
		},
		Retention: config.LogRetentionConfig{
			MaxAge:         config.Duration(24 * time.Hour),
			MaxBackups:     2,
			MaxTotalSizeMB: 2,
		},
	}
}

func TestManagedLoggerSplitsLevelsAndUsesFixedPath(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	managed, err := newManagedLogger(dir, testLogConfig(), &console)
	if err != nil {
		t.Fatal(err)
	}
	logger := managed.Logger()
	logger.Info("file only message")
	managed.ConsoleInfo("startup status", "log_file", managed.LogPath())
	logger.Warn("shared warning")
	if err := managed.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "logs", "mast.log"))
	if err != nil {
		t.Fatal(err)
	}
	fileLogs := string(data)
	if !strings.Contains(fileLogs, `"msg":"file only message"`) || !strings.Contains(fileLogs, `"msg":"shared warning"`) {
		t.Fatalf("unexpected file logs: %s", fileLogs)
	}
	if strings.Contains(fileLogs, "startup status") {
		t.Fatalf("console-only startup status leaked into file logs: %s", fileLogs)
	}
	if strings.Contains(console.String(), "file only message") || !strings.Contains(console.String(), "startup status") || !strings.Contains(console.String(), "shared warning") {
		t.Fatalf("unexpected console logs: %s", console.String())
	}
	if !filepath.IsAbs(managed.LogPath()) || managed.LogPath() != filepath.Join(dir, "logs", "mast.log") {
		t.Fatalf("log path = %q", managed.LogPath())
	}
}

func TestManagedLoggerHonorsFileLevelAndTextFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := testLogConfig()
	cfg.Level = "error"
	cfg.Format = "text"
	managed, err := newManagedLogger(dir, cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	managed.Logger().Warn("excluded warning")
	managed.Logger().Error("included error", slog.String("kind", "test"))
	if err := managed.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "logs", "mast.log"))
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	if strings.Contains(logs, "excluded warning") || !strings.Contains(logs, "[ERROR] - included error kind=test") {
		t.Fatalf("unexpected text logs: %s", logs)
	}
}

func TestLineHandlerFormat(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(newLineHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("some message", "tag1", 1, "tag2", 2)
	line := output.String()
	if !strings.Contains(line, " [INFO] - some message tag1=1 tag2=2\n") {
		t.Fatalf("line = %q", line)
	}
	if len(line) < len("2006-01-02 15:04:05.000 [INFO] - some message tag1=1 tag2=2\n") {
		t.Fatalf("line too short: %q", line)
	}
	stamp := line[:len("2006-01-02 15:04:05.000")]
	if _, err := time.ParseInLocation("2006-01-02 15:04:05.000", stamp, time.Local); err != nil {
		t.Fatalf("timestamp %q: %v", stamp, err)
	}
}

func TestManagedLoggerRejectsUnavailableLogDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newManagedLogger(dir, testLogConfig(), &bytes.Buffer{}); err == nil {
		t.Fatal("unavailable log directory should fail")
	}
}

func TestMegabytesToBytesRejectsOverflow(t *testing.T) {
	overflow := uint64(maxSizeMB) + 1
	if overflow > uint64(^uint(0)>>1) {
		t.Skip("int width cannot represent an overflowing megabyte value")
	}
	if _, err := megabytesToBytes(int(overflow)); err == nil {
		t.Fatal("overflowing megabyte value should fail")
	}
}
