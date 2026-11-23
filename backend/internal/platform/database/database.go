// Package database owns the PostgreSQL connection pool, transaction helpers and
// the embedded migration runner.
//
// Migrations are embedded in the binary and applied by the `migrate` command in
// CI/CD, never by hand on a production host. A schema-version table records every
// applied migration and a checksum prevents silent edits to already-applied files.
package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repositories
// participate in a caller-managed transaction without knowing which one they hold.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB wraps the connection pool with the helpers the rest of the codebase uses.
type DB struct {
	pool *pgxpool.Pool
}

// Options configures the pool.
type Options struct {
	URL              string
	MaxConns         int32
	MinConns         int32
	MaxConnLifetime  time.Duration
	StatementTimeout time.Duration
	ApplicationName  string
}

// Open creates a pool and verifies connectivity.
func Open(ctx context.Context, opts Options) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		cfg.MinConns = opts.MinConns
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.ApplicationName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = opts.ApplicationName
	}
	if opts.StatementTimeout > 0 {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", opts.StatementTimeout.Milliseconds())
	}

	// Server-side prepared statement caching is disabled because the pool is
	// expected to sit behind PgBouncer in transaction pooling mode.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: open pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Pool exposes the underlying pool for code that needs it directly.
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

// Close releases all connections.
func (d *DB) Close() { d.pool.Close() }

// Ping verifies connectivity, used by the readiness probe.
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// Stat reports pool statistics for the metrics endpoint.
func (d *DB) Stat() map[string]any {
	s := d.pool.Stat()
	return map[string]any{
		"acquired":          s.AcquiredConns(),
		"idle":              s.IdleConns(),
		"total":             s.TotalConns(),
		"max":               s.MaxConns(),
		"acquire_count":     s.AcquireCount(),
		"empty_acquires":    s.EmptyAcquireCount(),
		"canceled_acquires": s.CanceledAcquireCount(),
	}
}

// InTx runs fn inside a transaction. The transaction is rolled back unless fn
// returns nil; panics trigger a rollback and propagate.
func (d *DB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("database: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("database: rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit: %w", err)
	}
	return nil
}

// IsUniqueViolation reports whether err is a PostgreSQL unique-constraint error.
// Repositories use it to turn a race into a domain-level "already exists".
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// IsForeignKeyViolation reports whether err is a foreign-key violation.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// IsCheckViolation reports whether err is a check-constraint violation.
func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}
	return false
}

// ---------------------------------------------------------------------------
// Migration runner
// ---------------------------------------------------------------------------

type migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

var errMigrationDrift = errors.New("database: migration checksum drift")

// Migrate applies every pending migration found in dir (sorted by numeric
// prefix) and returns the migration versions applied during this run.
func (d *DB) Migrate(ctx context.Context, files fs.FS, dir string) ([]int64, error) {
	if _, err := d.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version      BIGINT PRIMARY KEY,
			name         TEXT NOT NULL,
			checksum     TEXT NOT NULL,
			applied_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			execution_ms BIGINT NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("database: ensure schema_migrations: %w", err)
	}

	migrations, err := loadMigrations(files, dir)
	if err != nil {
		return nil, err
	}

	applied := map[int64]string{}
	rows, err := d.pool.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("database: read applied migrations: %w", err)
	}
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return nil, fmt.Errorf("database: scan applied migration: %w", err)
		}
		applied[version] = checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: iterate applied migrations: %w", err)
	}

	var newlyApplied []int64
	for _, m := range migrations {
		if existing, ok := applied[m.Version]; ok {
			if existing != m.Checksum {
				return nil, fmt.Errorf("%w: version %d (%s) was edited after being applied", errMigrationDrift, m.Version, m.Name)
			}
			continue
		}
		start := time.Now()
		if err := d.applyOne(ctx, m); err != nil {
			return newlyApplied, err
		}
		newlyApplied = append(newlyApplied, m.Version)
		_ = start
	}
	return newlyApplied, nil
}

func (d *DB) applyOne(ctx context.Context, m migration) error {
	return d.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// A session-level advisory lock serialises concurrent deploys. The lock is
		// released automatically when the transaction ends.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x636F6E74656E74)); err != nil {
			return fmt.Errorf("database: acquire migration lock: %w", err)
		}
		start := time.Now()
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("database: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_migrations (version, name, checksum, execution_ms)
			VALUES ($1, $2, $3, $4)`,
			m.Version, m.Name, m.Checksum, time.Since(start).Milliseconds()); err != nil {
			return fmt.Errorf("database: record migration %d: %w", m.Version, err)
		}
		return nil
	})
}

func loadMigrations(files fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, fmt.Errorf("database: read migrations dir %q: %w", dir, err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, name, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		content, err := fs.ReadFile(files, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("database: read migration %q: %w", e.Name(), err)
		}
		sum := sha256.Sum256(content)
		out = append(out, migration{
			Version:  version,
			Name:     name,
			SQL:      string(content),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i := 1; i < len(out); i++ {
		if out[i].Version == out[i-1].Version {
			return nil, fmt.Errorf("database: duplicate migration version %d", out[i].Version)
		}
	}
	return out, nil
}

func parseMigrationName(filename string) (int64, string, error) {
	base := strings.TrimSuffix(filename, ".sql")
	prefix, rest, ok := strings.Cut(base, "_")
	if !ok {
		return 0, "", fmt.Errorf("database: migration %q must be named <version>_<name>.sql", filename)
	}
	var version int64
	if _, err := fmt.Sscanf(prefix, "%d", &version); err != nil || version <= 0 {
		return 0, "", fmt.Errorf("database: migration %q has a non-numeric version", filename)
	}
	return version, strings.ReplaceAll(rest, "_", " "), nil
}
