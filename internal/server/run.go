package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"example.com/server-golang-template/internal/config"
)

// Run owns the listener and waits for in-flight requests during shutdown.
func Run(ctx context.Context, c config.Config, logger *slog.Logger, opts ...Option) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", c.Address())
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.Info("server listening", "address", listener.Addr().String())
	err = serve(ctx, listener, c, New(c, logger, opts...))
	if err != nil {
		logger.Error("server stopped with error")
	} else {
		logger.Info("server stopped")
	}
	return err
}
func serve(ctx context.Context, listener net.Listener, c config.Config, handler http.Handler) error {
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), c.ShutdownTimeout)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			_ = srv.Close()
		}
		serveErr := <-done
		if err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve during shutdown: %w", serveErr)
		}
		return nil
	}
}
