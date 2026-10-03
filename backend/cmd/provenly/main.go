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
	os.Exit(run(os.Args[1:]))
}

// run executes the CLI with production dependencies until SIGINT/SIGTERM.
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, args, cli.DefaultDeps(os.Getenv, os.Stderr))
}
