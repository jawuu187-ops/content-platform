package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newStore(t *testing.T) *LocalStore {
	t.Helper()
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	return store
}

func TestIngestAcceptsOnlyRealImagesAndMP4(t *testing.T) {
	store := newStore(t)
	limits := DefaultLimits()

	obj, err := Ingest(context.Background(), store, bytes.NewReader(pngBytes(t, 20, 10)), limits)
	if err != nil {
		t.Fatalf("png ingest: %v", err)
	}
	if obj.ContentType != "image/png" || obj.Kind != KindImage || obj.Width != 20 || obj.Height != 10 {
		t.Fatalf("unexpected object: %+v", obj)
	}
	if obj.ByteSize <= 0 || len(obj.SHA256) != 64 {
		t.Fatalf("missing size or hash: %+v", obj)
	}

	rejects := []struct {
		name string
		data []byte
		want error
	}{
		{"html", []byte("<html><script>alert(1)</script></html>"), ErrUnsupportedType},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ErrUnsupportedType},
		{"avif", append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f'}, make([]byte, 64)...), ErrUnsupportedType},
		{"png header with garbage", append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("not really a png")...), ErrInvalidFile},
	}
	for _, tc := range rejects {
		if _, err := Ingest(context.Background(), store, bytes.NewReader(tc.data), limits); !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
}

func TestIngestRejectsOversizedImage(t *testing.T) {
	store := newStore(t)
	limits := Limits{ImageBytes: 128, VideoBytes: 1024, MaxPixels: 1_000_000}
	if _, err := Ingest(context.Background(), store, bytes.NewReader(pngBytes(t, 40, 40)), limits); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestStorageKeysAreHashesAndTraversalFails(t *testing.T) {
	store := newStore(t)
	key, sha, size, err := store.Put(context.Background(), bytes.NewReader(pngBytes(t, 8, 8)), 1<<20)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if key != sha || len(key) != 64 || size <= 0 {
		t.Fatalf("unexpected key: %q sha=%q size=%d", key, sha, size)
	}
	// The same bytes land on the same key: content addressing, not naming.
	again, _, _, err := store.Put(context.Background(), bytes.NewReader(pngBytes(t, 8, 8)), 1<<20)
	if err != nil || again != key {
		t.Fatalf("content addressing did not deduplicate: %q vs %q err=%v", again, key, err)
	}
	for _, bad := range []string{"../../etc/passwd", "deadbeef", strings.Repeat("z", 64), ""} {
		if _, _, err := store.Open(context.Background(), bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: want ErrInvalidInput, got %v", bad, err)
		}
	}
	body, n, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = body.Close() }()
	if n != size {
		t.Fatalf("size mismatch: %d vs %d", n, size)
	}
}

func TestLocalStoreRejectsOversizedStreams(t *testing.T) {
	store := newStore(t)
	if _, _, _, err := store.Put(context.Background(), bytes.NewReader(make([]byte, 2048)), 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	// Nothing is left behind as a readable object.
	entries, err := os.ReadDir(filepath.Join(store.root, "tmp"))
	if err != nil {
		t.Fatalf("read tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a rejected upload left %d temp files", len(entries))
	}
}

func TestUnavailableStorageFailsClosed(t *testing.T) {
	if _, err := Ingest(context.Background(), Unavailable{}, bytes.NewReader(pngBytes(t, 4, 4)), DefaultLimits()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if _, _, err := (Unavailable{}).Open(context.Background(), strings.Repeat("a", 64)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("open: want ErrUnavailable, got %v", err)
	}
}
