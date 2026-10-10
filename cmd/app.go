package cmd

import "github.com/urfave/cli/v3"

func New() *cli.Command {
	return &cli.Command{
		Name:  "golink",
		Usage: "golink service on Tailscale",
		Commands: []*cli.Command{
			startCmd(),
		},
	}
}
