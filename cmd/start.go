package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/urfave/cli/v3"

	"github.com/holi0317/golink/server"
)

func startCmd() *cli.Command {
	var flagPort uint16
	var flagDatabase string

	return &cli.Command{
		Name:  "start",
		Usage: "Start golink server",
		Flags: []cli.Flag{
			&cli.Uint16Flag{
				Name:        "port",
				Value:       8080,
				Destination: &flagPort,
			},
			&cli.StringFlag{
				// Falls back to the DATABASE_URL environment variable; both
				// plain --database (miss) and empty env are an error so the
				// server never boots without knowing where its data is.
				Name:        "database",
				Sources:     cli.EnvVars("DATABASE_URL"),
				Required:    true,
				Destination: &flagDatabase,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			pool, err := server.NewPool(ctx, flagDatabase)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer pool.Close()
			log := clog.FromContext(ctx)
			log.Info("database pool ready")

			addr := net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(flagPort), 10))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listening on %s: %w", addr, err)
			}
			clog.FromContext(ctx).Info("golink listening", "addr", ln.Addr().String())

			srv := &http.Server{Handler: server.New(
				pool,
				server.ClogProvider(clog.FromContext(ctx)),
				server.RequestLogger(),
			)}
			serveErr := make(chan error, 1)
			go func() { serveErr <- srv.Serve(ln) }()

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()

			select {
			case err := <-serveErr:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				if err != nil {
					return fmt.Errorf("serving: %w", err)
				}
			case <-ctx.Done():
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("graceful shutdown: %w", err)
			}
			return nil
		},
	}
}
