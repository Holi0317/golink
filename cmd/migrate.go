package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/holi0317/golink/db/migrations"
)

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
					db, err := migrations.New(flagDatabase)
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
					db, err := migrations.New(flagDatabase)
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
