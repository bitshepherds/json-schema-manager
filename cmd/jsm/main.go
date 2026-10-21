package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/bitshepherds/json-schema-manager/internal/app"
)

func main() {
	// Create context that cancels on SIGINT (Ctrl+C) or SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := app.Run(ctx, os.Args, os.Stdout, os.Stderr, nil)
	stop()

	os.Exit(app.ExitCode(err))
}
