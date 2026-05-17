package db

import "embed"

// migrationsFS embeds the SQL files under /migrations so they ship
// inside the compiled binary. The single-binary deployment model in
// the architecture overview depends on this — operators should never
// need to copy SQL files alongside the binary.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
