package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder for dimension checks
	_ "image/png"  // register the PNG decoder for dimension checks
	"io"
)

// Accepted container brands for MP4. Anything else (QuickTime, Matroska, AVI)
// is refused rather than guessed at.
var mp4Brands = map[string]bool{
	"isom": true, "iso2": true, "iso5": true, "iso6": true,
	"mp41": true, "mp42": true, "avc1": true, "dash": true, "M4V ": true,
}

// Ingest validates a stream and stores it. Validation happens against the bytes,
// never against a client-supplied content type: the declared header is ignored
// on purpose, because it is the easiest thing for an attacker to control.
func Ingest(ctx context.Context, st Storage, r io.Reader, limits Limits) (Object, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	head, err := br.Peek(12)
	if err != nil {
		return Object{}, fmt.Errorf("%w: file is too short to identify", ErrInvalidFile)
	}
	switch {
	case isJPEG(head):
		return ingestImage(ctx, st, br, limits)
	case isPNG(head):
		return ingestImage(ctx, st, br, limits)
	case isMP4(head):
		return ingestVideo(ctx, st, br, limits)
	default:
		return Object{}, ErrUnsupportedType
	}
}

func isJPEG(head []byte) bool {
	return len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
}

func isPNG(head []byte) bool {
	return len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
}

// isMP4 checks the box structure rather than a filename: the first box must be
// "ftyp" with a plausible size and an accepted brand.
func isMP4(head []byte) bool {
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return false
	}
	size := binary.BigEndian.Uint32(head[0:4])
	if size < 16 || size > 4096 {
		return false
	}
	return mp4Brands[string(head[8:12])]
}

func ingestImage(ctx context.Context, st Storage, br *bufio.Reader, limits Limits) (Object, error) {
	limit := limits.ImageBytes
	if limit <= 0 {
		limit = DefaultLimits().ImageBytes
	}
	data, err := io.ReadAll(io.LimitReader(br, limit+1))
	if err != nil {
		return Object{}, fmt.Errorf("media: read image: %w", err)
	}
	if int64(len(data)) > limit {
		return Object{}, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Object{}, fmt.Errorf("%w: the image header could not be decoded", ErrInvalidFile)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 20000 || cfg.Height > 20000 {
		return Object{}, fmt.Errorf("%w: implausible image dimensions", ErrInvalidFile)
	}
	maxPixels := limits.MaxPixels
	if maxPixels <= 0 {
		maxPixels = DefaultLimits().MaxPixels
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return Object{}, fmt.Errorf("%w: image is larger than the pixel budget", ErrInvalidFile)
	}
	contentType := "image/jpeg"
	if format == "png" {
		contentType = "image/png"
	}
	key, sha, size, err := st.Put(ctx, bytes.NewReader(data), limit)
	if err != nil {
		return Object{}, err
	}
	return Object{
		Kind: KindImage, ContentType: contentType, ByteSize: size,
		Width: cfg.Width, Height: cfg.Height, StorageKey: key, SHA256: sha,
	}, nil
}

func ingestVideo(ctx context.Context, st Storage, br *bufio.Reader, limits Limits) (Object, error) {
	limit := limits.VideoBytes
	if limit <= 0 {
		limit = DefaultLimits().VideoBytes
	}
	key, sha, size, err := st.Put(ctx, br, limit)
	if err != nil {
		return Object{}, err
	}
	return Object{
		Kind: KindVideo, ContentType: "video/mp4", ByteSize: size,
		StorageKey: key, SHA256: sha,
	}, nil
}
