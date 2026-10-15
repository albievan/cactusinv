// Package migrations embeds the goose SQL migrations so the binary can apply them.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
