// Package migrations embeds the Goose SQL migrations.
package migrations

import "embed"

// FS holds every *.sql migration, at the root of the file system.
//
//go:embed *.sql
var FS embed.FS
