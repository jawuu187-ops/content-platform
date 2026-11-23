// Package migrations embeds the SQL migration files so a binary can migrate a
// database without shipping loose files. The Go runner
// (internal/platform/database) applies them in numeric order inside a
// transaction and refuses to run if an already-applied file was edited.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
