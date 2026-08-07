package baselinesync

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/baseline"
)

// Manager 管理 baseline 的首次同步、原子切换和后台周期同步。
type Manager struct {
	client        Client
	store         *baseline.Store
	activePath    string
	checkInterval time.Duration
	instanceID    string
	logger        *slog.Logger
	metadata      MetadataStore

	syncMu sync.Mutex
	mu     sync.Mutex
	closed bool
}

// MetadataStore 保存 baseline 当前指针等运行时元数据。
type MetadataStore interface {
	SetMetadata(context.Context, string, string) error
}

const ActivePathMetadataKey = "baseline_active_path"

// Options 配置 baseline 同步管理器。
type Options struct {
	Client        Client
	Store         *baseline.Store
	ActivePath    string
	CheckInterval time.Duration
	InstanceID    string
	Logger        *slog.Logger
	Metadata      MetadataStore
}

func NewManager(options Options) (*Manager, error) {
	if options.Client == nil || options.Store == nil {
		return nil, errors.New("baseline client and store are required")
	}
	if options.ActivePath == "" {
		return nil, errors.New("baseline active path is required")
	}
	if options.CheckInterval <= 0 {
		options.CheckInterval = 24 * time.Hour
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Manager{client: options.Client, store: options.Store, activePath: options.ActivePath, checkInterval: options.CheckInterval, instanceID: options.InstanceID, logger: options.Logger, metadata: options.Metadata}, nil
}

func (m *Manager) Sync(ctx context.Context) error {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("baseline manager is closed")
	}
	current := m.store.ActiveVersion()
	manifest, err := m.client.Check(ctx, current)
	if err != nil {
		return err
	}
	if !manifest.HasUpdate {
		return nil
	}
	dir := filepath.Dir(m.activePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}
	archivePath, err := downloadArchiveWithContext(ctx, m.client, manifest, dir)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)
	databasePath, err := extractDatabase(archivePath, dir)
	if err != nil {
		return err
	}
	defer os.Remove(databasePath)
	if err := validateDatabase(databasePath, manifest.LatestVersion); err != nil {
		return err
	}
	oldPath := m.store.ActivePath()
	finalPath := filepath.Join(dir, fmt.Sprintf("baseline-%s-%d.db", manifest.LatestVersion, time.Now().UnixNano()))
	if err := activateDatabase(ctx, m.store, databasePath, finalPath, oldPath, m.metadata); err != nil {
		return err
	}
	m.mu.Lock()
	m.activePath = finalPath
	m.mu.Unlock()
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	jitter := stableJitter(m.instanceID, m.checkInterval)
	timer := time.NewTimer(m.checkInterval + jitter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if err := m.Sync(ctx); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Error("baseline synchronization failed", "error", err)
			}
			timer.Reset(m.checkInterval + jitter)
		}
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	return nil
}

func stableJitter(instanceID string, interval time.Duration) time.Duration {
	if interval <= 0 || instanceID == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = io.WriteString(h, instanceID)
	return time.Duration(uint64(h.Sum32()) % uint64(interval/10+1))
}

func downloadArchiveWithContext(ctx context.Context, client Client, manifest Manifest, dir string) (string, error) {
	if manifest.SizeBytes <= 0 || manifest.SizeBytes > maxArchiveSize {
		return "", fmt.Errorf("baseline archive size exceeds limit")
	}
	file, err := os.CreateTemp(dir, ".baseline-archive-*.zip")
	if err != nil {
		return "", fmt.Errorf("create baseline archive temp file: %w", err)
	}
	path := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	limited := &limitedWriter{writer: file, remaining: maxArchiveSize + 1}
	n, err := client.Download(ctx, manifest.DownloadURL, limited)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if n != manifest.SizeBytes || n > maxArchiveSize {
		return "", errors.New("baseline archive size mismatch")
	}
	if err := verifySHA256(path, manifest.Checksum); err != nil {
		return "", err
	}
	remove = false
	return path, nil
}

func activateDatabase(ctx context.Context, store *baseline.Store, candidate, active, previous string, metadata MetadataStore) error {
	if err := ensureFile(candidate); err != nil {
		return err
	}
	if err := os.Rename(candidate, active); err != nil {
		return fmt.Errorf("activate baseline file: %w", err)
	}
	if metadata != nil {
		if err := metadata.SetMetadata(ctx, ActivePathMetadataKey, active); err != nil {
			_ = os.Remove(active)
			return fmt.Errorf("persist baseline active path: %w", err)
		}
	}
	if err := store.Replace(active); err != nil {
		return fmt.Errorf("activate baseline store: %w", err)
	}
	if previous != "" && previous != active {
		_ = os.Remove(previous)
	}
	return nil
}
