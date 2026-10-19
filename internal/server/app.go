// Package server composes the application without starting a listener.
package server

import (
	"log/slog"
	"net/http"

	"example.com/server-golang-template/internal/auth"
	"example.com/server-golang-template/internal/config"
	"example.com/server-golang-template/internal/features/health"
	"example.com/server-golang-template/internal/httpapi"
)

type options struct{ verifier auth.Verifier }

// Option configures application dependencies.
type Option func(*options)

// WithVerifier installs the concurrency-safe token verifier used by protected routes.
// Without this option protected routes fail closed.
func WithVerifier(verifier auth.Verifier) Option { return func(o *options) { o.verifier = verifier } }

// New expects configuration validated by config.Load and a non-nil logger.
func New(c config.Config, logger *slog.Logger, opts ...Option) http.Handler {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	routes := applicationRoutes(c, o.verifier)
	return httpapi.Middleware(c, logger, httpapi.Router(routes), httpapi.WithRateLimitExemption(health.IsLivenessRequest))
}
