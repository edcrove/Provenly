// Package server runs an HTTP handler until its context is cancelled.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ShutdownTimeout bounds graceful shutdown.
const ShutdownTimeout = 10 * time.Second

// Timeouts against slow clients holding connections open (slowloris): a request (headers and body, up to a 10 MiB
// report) must arrive within ReadTimeout, and an idle keep-alive connection is closed after IdleTimeout.
var (
	ReadHeaderTimeout = 10 * time.Second
	ReadTimeout       = 2 * time.Minute
	IdleTimeout       = 2 * time.Minute
)

// Run serves handler on listener until ctx is done, then shuts down gracefully
// waiting at most shutdownTimeout for in-flight requests.
func Run(ctx context.Context, listener net.Listener, handler http.Handler, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		IdleTimeout:       IdleTimeout,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(listener) }()
	slog.Info("http server listening", "addr", listener.Addr().String())
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	<-errc
	slog.Info("http server stopped", "error", err)
	return err
}
