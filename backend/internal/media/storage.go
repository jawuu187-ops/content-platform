package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var keyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// LocalStore keeps objects on a local filesystem, content addressed by SHA-256.
//
// It is the driver used in CI and on a single-node deployment. It is not a CDN
// and it does not replicate; an object-store driver can replace it without a
// change above the Storage interface.
type LocalStore struct {
	root string
}

// NewLocalStore prepares the directory tree and returns the driver.
func NewLocalStore(root string) (*LocalStore, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: storage directory is required", ErrInvalidInput)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("media: resolve storage directory: %w", err)
	}
	for _, dir := range []string{abs, filepath.Join(abs, "tmp")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("media: create storage directory: %w", err)
		}
	}
	return &LocalStore{root: abs}, nil
}

// Name reports the driver name.
func (s *LocalStore) Name() string { return "local" }

// Put streams the content, rejects it above the limit, and files it under its
// content hash. A partially written upload never becomes visible: it is renamed
// into place only after the write succeeds.
func (s *LocalStore) Put(ctx context.Context, r io.Reader, limit int64) (string, string, int64, error) {
	if limit <= 0 {
		return "", "", 0, fmt.Errorf("%w: a positive limit is required", ErrInvalidInput)
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "put-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("media: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	h := sha256.New()
	// One byte over the limit is enough to know it was exceeded.
	written, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, limit+1))
	if err != nil {
		cleanup()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", "", 0, err
		}
		return "", "", 0, fmt.Errorf("media: write object: %w", err)
	}
	if written > limit {
		cleanup()
		return "", "", 0, ErrTooLarge
	}
	if written == 0 {
		cleanup()
		return "", "", 0, fmt.Errorf("%w: empty file", ErrInvalidFile)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", "", 0, fmt.Errorf("media: sync object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", "", 0, fmt.Errorf("media: close object: %w", err)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(s.root, sum[:2])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		_ = os.Remove(tmpName)
		return "", "", 0, fmt.Errorf("media: create shard directory: %w", err)
	}
	final := filepath.Join(dir, sum)
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return "", "", 0, fmt.Errorf("media: finalise object: %w", err)
	}
	return sum, sum, written, nil
}

// Open returns the stored bytes for a key. The key must be a hash, which is what
// makes a traversal attempt fail before touching the filesystem.
func (s *LocalStore) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	path, err := s.pathOf(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("media: open object: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("media: stat object: %w", err)
	}
	return f, info.Size(), nil
}

// Stat reports the stored size.
func (s *LocalStore) Stat(_ context.Context, key string) (Stat, error) {
	path, err := s.pathOf(key)
	if err != nil {
		return Stat{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Stat{}, ErrNotFound
	}
	if err != nil {
		return Stat{}, fmt.Errorf("media: stat object: %w", err)
	}
	return Stat{Key: key, Size: info.Size()}, nil
}

func (s *LocalStore) pathOf(key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", fmt.Errorf("%w: malformed storage key", ErrInvalidInput)
	}
	return filepath.Join(s.root, key[:2], key), nil
}

// Unavailable is the driver used when no storage is configured: every operation
// fails closed instead of writing somewhere unexpected.
type Unavailable struct{}

// Name reports the unconfigured driver.
func (Unavailable) Name() string { return "unconfigured" }

// Put always fails.
func (Unavailable) Put(context.Context, io.Reader, int64) (string, string, int64, error) {
	return "", "", 0, ErrUnavailable
}

// Open always fails.
func (Unavailable) Open(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, ErrUnavailable
}

// Stat always fails.
func (Unavailable) Stat(context.Context, string) (Stat, error) { return Stat{}, ErrUnavailable }
