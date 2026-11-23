// Command migrate applies pending database migrations.
//
// Usage:
//
//	migrate            apply all pending migrations
//	migrate -status    print applied and pending versions without changing anything
//
// The command is safe to run from CI immediately before a deploy: a Postgres
// advisory lock serialises concurrent runs, and each file is applied inside a
// transaction together with its schema_migrations row.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/config"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
	"github.com/jawuu187-ops/content-platform/backend/migrations"
)

func main() {
	statusOnly := flag.Bool("status", false, "print migration status without applying")
	flag.Parse()

	if err := run(*statusOnly); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(statusOnly bool) error {
	cfg, err := config.Load("migrate")
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := database.Open(ctx, database.Options{
		URL:              cfg.DatabaseURL,
		MaxConns:         2,
		MinConns:         1,
		StatementTimeout: 0, // migrations may legitimately run long
		ApplicationName:  "migrate",
	})
	if err != nil {
		return err
	}
	defer db.Close()

	sub, err := fs.Sub(migrations.FS, ".")
	if err != nil {
		return err
	}

	if statusOnly {
		return printStatus(ctx, db)
	}

	applied, err := db.Migrate(ctx, sub, ".")
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		slog.Info("database is up to date")
		return nil
	}
	slog.Info("migrations applied", slog.Any("versions", applied))
	return nil
}

func printStatus(ctx context.Context, db *database.DB) error {
	rows, err := db.Pool().Query(ctx, `SELECT version, name, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	fmt.Println("applied:")
	for rows.Next() {
		var version int64
		var name string
		var appliedAt time.Time
		if err := rows.Scan(&version, &name, &appliedAt); err != nil {
			return err
		}
		fmt.Printf("  %04d %-40s %s\n", version, name, appliedAt.UTC().Format(time.RFC3339))
	}
	return rows.Err()
}
