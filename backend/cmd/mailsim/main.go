// Command mailsim inspects the local simulated mailbox. It is a developer tool:
// run it on the host that owns the local/test database, never against a public
// API. It prints message bodies because completing a verification or reset flow
// requires the code; do not paste its output into a public issue.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mailsim"
)

func main() {
	limit := flag.Int("limit", 20, "number of messages to list")
	jsonMode := flag.Bool("json", false, "print one message as JSON (body included)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	messages, err := mailsim.Latest(ctx, pool, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(messages) == 0 {
		fmt.Println("no simulated messages")
		return
	}
	for _, m := range messages {
		fmt.Printf("%s  %-18s  %-28s  %s\n", m.CreatedAt.UTC().Format(time.RFC3339), m.Kind, m.Recipient, m.Subject)
		if *jsonMode {
			fmt.Printf("  id=%s\n  %s\n", m.ID, m.Body)
		}
	}
}
