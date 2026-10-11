// Package migrations embeds the dbmate migration SQL files so ko-built
// images ship runnable migrations inside the single binary (golink-cqh).
package migrations

import "embed"

// FS is rooted at db/migrations; dbmate consumes it with MigrationsDir
// set to "." (dbmate db.FS docs: listing and reading both go through it).
//
//go:embed *.sql
var FS embed.FS
