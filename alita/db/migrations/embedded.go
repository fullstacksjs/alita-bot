package migrations

import "embed"

//go:embed sqlite/*.sql
var SQLiteMigrationsFS embed.FS
