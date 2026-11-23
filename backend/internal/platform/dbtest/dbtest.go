// Package dbtest provides the database fixture used by integration tests. It is
// not imported by any production binary; it exists so every package's tests can
// share one correct way to reach a migrated, empty database.
package dbtest

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
	"github.com/jawuu187-ops/content-platform/backend/migrations"
)

// migrationLockKey serialises migration runs between test binaries. `go test`
// runs packages in parallel processes, and the migration runner records versions
// after committing each file, so two processes applying the same pending file
// would race on the schema_migrations primary key. A session-level advisory lock
// held while migrating removes that race without slowing the tests down.
const migrationLockKey int64 = 7712_2026

var (
	once       sync.Once
	migrateErr error
)

// Pool returns a pool against TEST_DATABASE_URL with all migrations applied and
// the identity tables emptied. The test is skipped when the variable is unset, so
// `go test ./...` still works on a machine without PostgreSQL.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database integration test")
	}

	ctx := context.Background()
	db, err := database.Open(ctx, database.Options{
		URL:             url,
		MaxConns:        8,
		ApplicationName: "integration-tests",
	})
	if err != nil {
		t.Fatalf("dbtest: open: %v", err)
	}
	t.Cleanup(db.Close)

	once.Do(func() {
		conn, err := db.Pool().Acquire(ctx)
		if err != nil {
			migrateErr = err
			return
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
			migrateErr = err
			return
		}
		defer func() {
			_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey)
		}()
		_, migrateErr = db.Migrate(ctx, migrations.FS, ".")
	})
	if migrateErr != nil {
		t.Fatalf("dbtest: migrate: %v", migrateErr)
	}

	if _, err := db.Pool().Exec(ctx, `TRUNCATE identity.users CASCADE`); err != nil {
		t.Fatalf("dbtest: reset identity tables: %v", err)
	}
	return db.Pool()
}
