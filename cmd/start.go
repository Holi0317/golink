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

	return &cli.Command{
		Name:  "start",
		Usage: "Start golink server",
		Flags: []cli.Flag{
			&cli.Uint16Flag{
				Name:        "port",
				Value:       8080,
				Destination: &flagPort,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			addr := net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(flagPort), 10))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listening on %s: %w", addr, err)
			}
			clog.FromContext(ctx).Info("golink listening", "addr", ln.Addr().String())

			// The base logger from main, threaded into requests. Derive it
			// from ctx (which carries nothing but the logger at this point)
			// before the signal context rebinds it.
			srv := &http.Server{Handler: server.ClogProvider(clog.FromContext(ctx))(server.New())}
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
