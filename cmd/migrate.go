package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
	_ "github.com/amacneil/dbmate/v2/pkg/driver/postgres"
	"github.com/urfave/cli/v3"

	"github.com/holi0317/golink/db/migrations"
)

// newDbmate builds a dbmate.DB running migrations from the embedded FS.
// Dev (mise tasks) and prod run this exact codepath; the raw dbmate binary
// was removed so it cannot drift (golink-cqh).
func newDbmate(databaseURL string) (*dbmate.DB, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	if u.Scheme == "" {
		return nil, dbmate.ErrInvalidURL
	}

	db := dbmate.New(u)
	db.FS = migrations.FS
	db.MigrationsDir = []string{"."}
	// Schema dumps are a dev-time concern dbmate used to cover via
	// DBMATE_SCHEMA_FILE=/dev/null; we never want one.
	db.AutoDumpSchema = false
	db.Log = os.Stdout
	return db, nil
}

func migrateCmd() *cli.Command {
	var flagDatabase string

	databaseFlag := func() cli.Flag {
		// Same contract as the start command: falls back to DATABASE_URL,
		// missing both is an error.
		return &cli.StringFlag{
			Name:        "database",
			Sources:     cli.EnvVars("DATABASE_URL"),
			Required:    true,
			Destination: &flagDatabase,
		}
	}

	return &cli.Command{
		Name:  "migrate",
		Usage: "Database migrations (embedded in the binary)",
		Commands: []*cli.Command{
			{
				Name:  "up",
				Usage: "Apply pending migrations; fails if the database does not exist",
				Flags: []cli.Flag{
					databaseFlag(),
					&cli.BoolFlag{
						// For deployment orchestration where the database may
						// still be booting (compose/k8s init container); dev
						// keeps failing fast.
						Name:    "wait",
						Usage:   "Wait for the database to become available before migrating",
						Sources: cli.EnvVars("MIGRATE_WAIT"),
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					db, err := newDbmate(flagDatabase)
					if err != nil {
						return err
					}
					db.WaitBefore = c.Bool("wait")
					return db.Migrate()
				},
			},
			{
				Name:  "status",
				Usage: "Show applied/pending migrations; exits 1 when pending migrations exist",
				Flags: []cli.Flag{
					databaseFlag(),
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					db, err := newDbmate(flagDatabase)
					if err != nil {
						return err
					}
					pending, err := db.Status(false)
					if err != nil {
						return err
					}
					if pending > 0 {
						return fmt.Errorf("%d pending migration(s)", pending)
					}
					return nil
				},
			},
		},
	}
}
