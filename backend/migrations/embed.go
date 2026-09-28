// Package migrations embeds the SQL schema migrations into the migration binary.
package migrations

import "embed"

// FS contains every ordered Goose migration.
//
//go:embed *.sql
var FS embed.FS
