package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const readHeaderTimeout = 10 * time.Second

// Serve serves h on l until ctx ends, then stops accepting new requests and
// lets in-flight ones finish for up to shutdownTimeout.
func Serve(ctx context.Context, l net.Listener, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	logger.Info("http server listening", "addr", l.Addr().String())

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down http server", "timeout", shutdownTimeout.String())
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}
