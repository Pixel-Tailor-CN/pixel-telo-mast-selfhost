package baselinesync

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxArchiveSize      = int64(512 << 20)
	maxDatabaseSize     = int64(2 << 30)
	maxCompressionRatio = int64(200)
)

func verifySHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open baseline archive for checksum: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash baseline archive: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(strings.TrimSpace(expected), actual) {
		return fmt.Errorf("baseline archive checksum mismatch")
	}
	return nil
}

func extractDatabase(archivePath, dir string) (string, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open baseline archive: %w", err)
	}
	defer archive.Close()
	var candidate *zip.File
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("baseline archive contains directory or symlink")
		}
		clean := filepath.Clean(filepath.FromSlash(entry.Name))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", errors.New("baseline archive contains unsafe path")
		}
		if filepath.Ext(clean) != ".db" {
			return "", errors.New("baseline archive contains unexpected file")
		}
		if candidate != nil {
			return "", errors.New("baseline archive contains multiple databases")
		}
		if entry.UncompressedSize64 > uint64(maxDatabaseSize) || (entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > uint64(maxCompressionRatio)) {
			return "", errors.New("baseline database exceeds extraction limits")
		}
		candidate = entry
	}
	if candidate == nil {
		return "", errors.New("baseline archive does not contain a database")
	}
	out, err := os.CreateTemp(dir, ".baseline-database-*.db")
	if err != nil {
		return "", fmt.Errorf("create baseline database temp file: %w", err)
	}
	path := out.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	reader, err := candidate.Open()
	if err != nil {
		out.Close()
		return "", fmt.Errorf("open archived baseline database: %w", err)
	}
	_, copyErr := io.Copy(out, io.LimitReader(reader, maxDatabaseSize+1))
	reader.Close()
	closeErr := out.Close()
	if copyErr != nil {
		return "", fmt.Errorf("extract baseline database: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close extracted baseline database: %w", closeErr)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxDatabaseSize {
		return "", errors.New("extracted baseline database exceeds limit")
	}
	remove = false
	return path, nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errors.New("write limit exceeded")
	}
	if int64(len(data)) > w.remaining {
		data = data[:w.remaining]
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	if err == nil && w.remaining == 0 {
		return n, errors.New("write limit exceeded")
	}
	return n, err
}
