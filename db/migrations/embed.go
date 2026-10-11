// Package migrations embeds the dbmate migration SQL files so ko-built
// images ship runnable migrations inside the single binary (golink-cqh).
package migrations

import (
	"embed"
	"fmt"
	"net/url"
	"os"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	// Registers the "postgres" scheme with dbmate's driver registry.
	_ "github.com/amacneil/dbmate/v2/pkg/driver/postgres"
)

// FS is rooted at db/migrations; dbmate consumes it with MigrationsDir
// set to "." (dbmate db.FS docs: listing and reading both go through it).
//
//go:embed *.sql
var FS embed.FS

// New builds a dbmate.DB running migrations from the embedded FS. Dev
// (mise tasks, tests) and prod run this exact codepath; the raw dbmate
// binary was removed so it cannot drift (golink-cqh).
func New(databaseURL string) (*dbmate.DB, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	if u.Scheme == "" {
		return nil, dbmate.ErrInvalidURL
	}

	db := dbmate.New(u)
	db.FS = FS
	db.MigrationsDir = []string{"."}
	// Schema dumps are a dev-time concern dbmate used to cover via
	// DBMATE_SCHEMA_FILE=/dev/null; we never want one.
	db.AutoDumpSchema = false
	db.Log = os.Stdout
	return db, nil
}
