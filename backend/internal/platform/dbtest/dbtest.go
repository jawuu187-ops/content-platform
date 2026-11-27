// Package dbtest provides the database fixture used by integration tests. It is
// not imported by any production binary; it exists so every package's tests get
// the same migrated, isolated database.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
	"github.com/jawuu187-ops/content-platform/backend/migrations"
)

// Each test binary (go test runs one per package, in parallel) gets its own
// database. Sharing one database made two packages truncate each other's rows
// and race on the migration version table. A private database removes both
// problems at the source instead of hiding them with locks.
var (
	once   sync.Once
	pool   *pgxpool.Pool
	setErr error
)

// Pool returns a pooled connection to a private, migrated database for this test
// process. Identity tables are emptied before each call so tests start clean.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database integration test")
	}

	once.Do(func() { setErr = setup(base) })
	if setErr != nil {
		t.Fatalf("dbtest: %v", setErr)
	}

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE identity.users CASCADE`); err != nil {
		t.Fatalf("dbtest: reset identity tables: %v", err)
	}
	return pool
}

// cleanupStale drops test databases left by a previous run. It skips the
// current process and any database with a live connection, so parallel package
// test binaries are never pulled out from under each other. Without this,
// repeated local runs accumulated until the filesystem filled.
func cleanupStale(ctx context.Context, admin *pgx.Conn, current string) {
	rows, err := admin.Query(ctx, `
		SELECT d.datname
		FROM pg_database d
		WHERE d.datname LIKE 'cp_test_%' AND d.datname <> $1
		  AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = d.datname)`, current)
	if err != nil {
		return
	}
	var stale []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			stale = append(stale, n)
		}
	}
	rows.Close()
	for _, n := range stale {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{n}.Sanitize())
	}
}

func setup(base string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// crypto/rand rather than math/rand: the suffix only needs to be distinct, but
	// a weak generator in a test helper is still flagged by static analysis and
	// would be copied into production code by accident.
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("generate test database name: %w", err)
	}
	name := fmt.Sprintf("cp_test_%d_%s", os.Getpid(), hex.EncodeToString(suffix[:]))

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return fmt.Errorf("connect to create the test database: %w", err)
	}
	cleanupStale(ctx, admin, name)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	_ = admin.Close(ctx)
	if err != nil {
		return fmt.Errorf("create test database %s: %w", name, err)
	}

	target, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	target.Path = "/" + name

	db, err := database.Open(ctx, database.Options{
		URL:             target.String(),
		MaxConns:        8,
		ApplicationName: "integration-tests",
	})
	if err != nil {
		return fmt.Errorf("open test database: %w", err)
	}
	if _, err := db.Migrate(ctx, migrations.FS, "."); err != nil {
		db.Close()
		return fmt.Errorf("migrate test database: %w", err)
	}
	pool = db.Pool()
	return nil
}
