package main

import (
	"context"
	"os"

	"github.com/bitshepherds/json-schema-manager/internal/app"
)

func main() {
	// Create context that cancels on SIGINT (Ctrl+C) or SIGTERM, which exit 130 and 143
	ctx, stop := app.NotifyContext(context.Background())

	err := app.Run(ctx, os.Args, os.Stdout, os.Stderr, nil)
	stop()

	os.Exit(app.ExitCode(err))
}
