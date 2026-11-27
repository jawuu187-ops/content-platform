// Package media ingests creator media, keeps it out of public reach, and issues
// single-use delivery tokens.
//
// Two rules shape the package:
//
//   - Storage keys are content hashes the service computes. A client never
//     supplies a path, so traversal and bucket-name leakage are structurally
//     impossible, not merely filtered.
//   - A delivery token is not an authorization decision. It records that a
//     decision was made; the decision is re-run when the token is redeemed, so a
//     cancelled subscription or a revoked verification closes access at the
//     delivery boundary.
package media

import (
	"context"
	"errors"
	"io"
	"time"
)

// Domain errors.
var (
	ErrUnavailable     = errors.New("media: storage is not configured")
	ErrUnsupportedType = errors.New("media: unsupported file type")
	ErrTooLarge        = errors.New("media: file exceeds the size limit")
	ErrInvalidFile     = errors.New("media: file content is not valid for its type")
	ErrNotFound        = errors.New("media: not found")
	ErrForbidden       = errors.New("media: not permitted")
	ErrTokenInvalid    = errors.New("media: delivery token is not valid")
	ErrTokenExpired    = errors.New("media: delivery token has expired")
	ErrTokenUsed       = errors.New("media: delivery token has already been used")
	ErrInvalidInput    = errors.New("media: invalid input")
)

// Kind is the coarse media category.
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
	KindAudio Kind = "audio"
)

// Limits are the ingestion budget. Values come from configuration so a
// deployment can tighten them without a code change.
type Limits struct {
	ImageBytes int64
	VideoBytes int64
	AudioBytes int64
	// MaxPixels rejects decompression-bomb dimensions before any decode of pixel
	// data happens.
	MaxPixels int64
}

// DefaultLimits are deliberately modest: a creator platform does not need 4K
// originals, and smaller files make delivery cheaper and faster.
func DefaultLimits() Limits {
	return Limits{ImageBytes: 10 << 20, VideoBytes: 200 << 20, AudioBytes: 60 << 20, MaxPixels: 40_000_000}
}

// Object is one stored file.
type Object struct {
	ID          string    `json:"id"`
	Kind        Kind      `json:"kind"`
	ContentType string    `json:"content_type"`
	ByteSize    int64     `json:"byte_size"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	DurationMS  int       `json:"duration_ms,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// StorageKey and SHA256 are internal: they are never serialised to a client.
	StorageKey string `json:"-"`
	SHA256     string `json:"-"`
}

// Stat is what a storage driver reports about a key.
type Stat struct {
	Key  string
	Size int64
}

// Storage is the replaceable storage seam. The only shipped driver writes to a
// local directory; an object-store driver implements the same three methods
// (put, open, stat) without touching the service or the handlers.
type Storage interface {
	Name() string
	// Put stores the stream and returns the content hash and byte count. The key
	// is derived from the hash by the driver, never from the caller.
	Put(ctx context.Context, r io.Reader, limit int64) (key, sha string, size int64, err error)
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Stat(ctx context.Context, key string) (Stat, error)
}

// Delivery is an issued, single-use download reference.
type Delivery struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Redeemed is the result of consuming a token: who may read what.
type Redeemed struct {
	Object   Object
	PostID   string
	ViewerID string
}
