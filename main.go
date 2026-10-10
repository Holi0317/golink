package main

import (
	"context"
	"os"

	"github.com/chainguard-dev/clog"
	"github.com/holi0317/golink/cmd"
)

// Base logger is built at the top level and threaded through the app via the
// context; handlers fetch it back with clog.FromContext (golink-ddl).
func main() {
	logger := clog.New(clog.NewHandler(nil))
	ctx := clog.WithLogger(context.Background(), logger)

	app := cmd.New()

	err := app.Run(ctx, os.Args)
	if err != nil {
		logger.Fatalf("%v", err)
	}
}
