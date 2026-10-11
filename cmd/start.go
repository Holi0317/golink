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

	"github.com/holi0317/golink/middleware"
	"github.com/holi0317/golink/server"
)

func startCmd() *cli.Command {
	var flagHost string
	var flagPort uint16
	var flagDatabase string
	var flagTailscaleIdentity bool

	return &cli.Command{
		Name:  "start",
		Usage: "Start golink server",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "host",
				Usage:       "host to listen on",
				Value:       "127.0.0.1",
				Sources:     cli.EnvVars("HOST"),
				Destination: &flagHost,
			},
			&cli.Uint16Flag{
				Name:        "port",
				Usage:       "port to listen on",
				Value:       8080,
				Sources:     cli.EnvVars("PORT"),
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
			&cli.BoolFlag{
				// Derive requester identity from the Tailscale-User-*
				// headers injected by tailscale serve (TAILSCALE_ENABLED
				// env, docs https://tailscale.com/s/serve-headers). Only
				// enable when the ONLY way to reach golink terminates at
				// tailscale serve; serve strips client-forged identity
				// headers, but nothing downstream does. golink already
				// default-binds to 127.0.0.1, which keeps this safe.
				Name:        "tailscale-identity",
				Usage:       "trust Tailscale-User-* headers injected by tailscale serve",
				Sources:     cli.EnvVars("TAILSCALE_ENABLED"),
				Destination: &flagTailscaleIdentity,
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

			addr := net.JoinHostPort(flagHost, strconv.FormatUint(uint64(flagPort), 10))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listening on %s: %w", addr, err)
			}
			clog.FromContext(ctx).Info("golink listening", "addr", ln.Addr().String())

			var mws []middleware.Middleware
			mws = append(mws,
				middleware.ClogProvider(clog.FromContext(ctx)),
				middleware.RequestLogger(),
			)
			if flagTailscaleIdentity {
				// Prepend so TailscaleIdentity wraps the whole chain and
				// identity is known to everything downstream.
				mws = append([]middleware.Middleware{middleware.TailscaleIdentity()}, mws...)
			}
			srv := &http.Server{Handler: server.New(
				pool,
				mws...,
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
