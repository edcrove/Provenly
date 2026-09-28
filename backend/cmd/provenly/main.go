// Command provenly runs the Provenly API server and its database migrations.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/edcrove/provenly/backend/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.DefaultDeps(os.Getenv, os.Stderr))
	stop()
	os.Exit(code)
}
