package main

import (
	"context"
	"log"
	"os"

	"github.com/holi0317/golink/cmd"
)

func main() {
	app := cmd.New()

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
