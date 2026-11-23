package media

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service ingests media and issues/redeems delivery tokens.
type Service struct {
	pool    *pgxpool.Pool
	storage Storage
	limits  Limits
	secret  []byte
	ttl     time.Duration
	now     func() time.Time
	logger  *slog.Logger
	// baseURL is used to build absolute delivery links when configured.
	baseURL string
}

// Option configures the service.
type Option func(*Service)

// WithStorage installs the storage driver.
func WithStorage(s Storage) Option { return func(svc *Service) { svc.storage = s } }

// WithLimits sets the ingestion budget.
func WithLimits(l Limits) Option { return func(svc *Service) { svc.limits = l } }

// WithSigningKey sets the HMAC key used to sign delivery tokens. It must be at
// least 32 bytes; the service refuses to issue tokens without it.
func WithSigningKey(key []byte) Option { return func(svc *Service) { svc.secret = key } }

// WithTokenTTL sets how long a delivery token lives. Short by design.
func WithTokenTTL(d time.Duration) Option { return func(svc *Service) { svc.ttl = d } }

// WithClock overrides the clock for tests.
func WithClock(now func() time.Time) Option { return func(svc *Service) { svc.now = now } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(svc *Service) { svc.logger = l } }

// WithBaseURL sets the public origin used in delivery links.
func WithBaseURL(u string) Option {
	return func(svc *Service) { svc.baseURL = strings.TrimRight(u, "/") }
}

// New builds the service.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{
		pool:    pool,
		storage: Unavailable{},
		limits:  DefaultLimits(),
		ttl:     60 * time.Second,
		now:     time.Now,
		logger:  slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Storage reports the active driver name.
func (s *Service) Storage() string { return s.storage.Name() }

// Configured reports whether ingestion can work at all.
func (s *Service) Configured() bool {
	return s.storage.Name() != "unconfigured" && len(s.secret) >= 32
}

// Ingest validates and stores an upload for an account, then records it.
func (s *Service) Ingest(ctx context.Context, ownerID string, r io.Reader) (Object, error) {
	if !s.Configured() {
		return Object{}, ErrUnavailable
	}
	obj, err := Ingest(ctx, s.storage, r, s.limits)
	if err != nil {
		return Object{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO media.objects (owner_id, kind, storage_key, content_type, byte_size, sha256, width, height, duration_ms)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, NULLIF($7, 0), NULLIF($8, 0), NULLIF($9, 0))
		ON CONFLICT (owner_id, storage_key)
		DO UPDATE SET byte_size = EXCLUDED.byte_size
		RETURNING id::text, kind, content_type, byte_size, COALESCE(width, 0), COALESCE(height, 0),
		          COALESCE(duration_ms, 0), storage_key, sha256, created_at`,
		ownerID, string(obj.Kind), obj.StorageKey, obj.ContentType, obj.ByteSize, obj.SHA256,
		obj.Width, obj.Height, obj.DurationMS)
	if err := row.Scan(&obj.ID, &obj.Kind, &obj.ContentType, &obj.ByteSize, &obj.Width, &obj.Height,
		&obj.DurationMS, &obj.StorageKey, &obj.SHA256, &obj.CreatedAt); err != nil {
		return Object{}, fmt.Errorf("media: record object: %w", err)
	}
	return obj, nil
}

// ObjectByID loads a ready object.
func (s *Service) ObjectByID(ctx context.Context, id string) (Object, error) {
	var o Object
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, kind, content_type, byte_size, COALESCE(width, 0), COALESCE(height, 0),
		       COALESCE(duration_ms, 0), storage_key, sha256, created_at
		FROM media.objects WHERE id = $1::uuid AND status = 'ready'`, id).
		Scan(&o.ID, &o.Kind, &o.ContentType, &o.ByteSize, &o.Width, &o.Height, &o.DurationMS,
			&o.StorageKey, &o.SHA256, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("media: load object: %w", err)
	}
	return o, nil
}

// ObjectOwnedBy reports whether the account owns the object.
func (s *Service) ObjectOwnedBy(ctx context.Context, id, ownerID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media.objects WHERE id = $1::uuid AND owner_id = $2::uuid AND status = 'ready')`,
		id, ownerID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("media: check object owner: %w", err)
	}
	return ok, nil
}

// Issue creates a single-use delivery token for a viewer and post. The caller
// must have already decided that the viewer may see the media; the decision is
// repeated when the token is redeemed.
func (s *Service) Issue(ctx context.Context, viewerID, mediaID, postID string) (Delivery, error) {
	if len(s.secret) < 32 {
		return Delivery{}, ErrUnavailable
	}
	obj, err := s.ObjectByID(ctx, mediaID)
	if err != nil {
		return Delivery{}, err
	}
	jti := newUUID()
	expires := s.now().UTC().Add(s.ttl)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO media.delivery_tokens (jti, media_id, post_id, viewer_id, issued_at, expires_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6)`,
		jti, obj.ID, postID, viewerID, s.now().UTC(), expires); err != nil {
		return Delivery{}, fmt.Errorf("media: issue token: %w", err)
	}
	token := jti + "." + s.sign(jti)
	link := "/v1/media/delivery/" + token
	if s.baseURL != "" {
		link = s.baseURL + link
	}
	return Delivery{URL: link, ExpiresAt: expires}, nil
}

// Redeem consumes a token exactly once and returns what it refers to. Every
// failure mode returns the same error so a caller cannot probe token state.
func (s *Service) Redeem(ctx context.Context, raw string) (Redeemed, error) {
	if len(s.secret) < 32 {
		return Redeemed{}, ErrUnavailable
	}
	jti, sig, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || jti == "" || sig == "" {
		return Redeemed{}, ErrTokenInvalid
	}
	if !hmac.Equal([]byte(sig), []byte(s.sign(jti))) {
		return Redeemed{}, ErrTokenInvalid
	}
	var out Redeemed
	err := s.pool.QueryRow(ctx, `
		UPDATE media.delivery_tokens
		SET redeemed_at = now()
		WHERE jti = $1::uuid AND redeemed_at IS NULL AND expires_at > now()
		RETURNING media_id::text, post_id::text, viewer_id::text`, jti).
		Scan(&out.Object.ID, &out.PostID, &out.ViewerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Redeemed{}, ErrTokenInvalid
	}
	if err != nil {
		return Redeemed{}, fmt.Errorf("media: redeem token: %w", err)
	}
	obj, err := s.ObjectByID(ctx, out.Object.ID)
	if err != nil {
		return Redeemed{}, err
	}
	out.Object = obj
	return out, nil
}

// Open streams a stored object.
func (s *Service) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	return s.storage.Open(ctx, key)
}

// Sweep deletes expired tokens. Called from tests now and from a scheduler later.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM media.delivery_tokens WHERE expires_at < now() - interval '1 hour'`)
	if err != nil {
		return 0, fmt.Errorf("media: sweep tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Service) sign(jti string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(jti))
	return hex.EncodeToString(mac.Sum(nil))
}

// newUUID builds a random version-4 UUID without a dependency.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("media: uuid entropy: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
