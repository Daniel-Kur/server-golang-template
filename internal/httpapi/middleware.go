package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/server-golang-template/internal/config"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }

var validID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type responseWriter struct {
	http.ResponseWriter
	status int
	head   bool
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.head {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

// FlushError preserves ResponseController semantics and tracks header commitment.
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type middlewareOptions struct {
	rateLimitExempt func(*http.Request) bool
}

// MiddlewareOption configures shared HTTP infrastructure without feature dependencies.
type MiddlewareOption func(*middlewareOptions)

// WithRateLimitExemption injects an application-owned exemption predicate.
// A nil predicate exempts nothing. The predicate must be safe for concurrent use;
// an exemption skips only quota accounting, not other middleware protections.
func WithRateLimitExemption(predicate func(*http.Request) bool) MiddlewareOption {
	return func(o *middlewareOptions) { o.rateLimitExempt = predicate }
}

// Middleware has no background goroutines. Its bounded fixed-window limiter is
// local to this handler instance and safe for concurrent requests.
func Middleware(c config.Config, logger *slog.Logger, next http.Handler, opts ...MiddlewareOption) http.Handler {
	var o middlewareOptions
	for _, opt := range opts {
		opt(&o)
	}
	c.CORSOrigins = append([]string(nil), c.CORSOrigins...)
	limiter := &rateLimiter{counts: make(map[string]int), max: c.RateLimitMax, capacity: c.RateLimitClients, window: c.RateLimitWindow}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !validID.MatchString(id) {
			id = rand.Text()
		}
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		rw := &responseWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
		aborted := false
		defer func() {
			status := rw.status
			if status == 0 && !aborted {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= 500 || aborted {
				level = slog.LevelError
			}
			// Never log raw URLs, headers, bodies, or panic values.
			logger.Log(r.Context(), level, "request completed", "request_id", id,
				"method", r.Method, "route", r.Pattern, "status", status,
				"aborted", aborted, "duration_ms", time.Since(start).Milliseconds())
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				panicErr, _ := recovered.(error)
				if errors.Is(panicErr, http.ErrAbortHandler) || rw.status != 0 {
					aborted = true
					panic(http.ErrAbortHandler)
				}
				// Discard stale entity headers before replacing an uncommitted response.
				for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Disposition", "ETag", "Last-Modified", "Location"} {
					rw.Header().Del(name)
				}
				Failure(rw, 500, "INTERNAL_SERVER_ERROR", "Internal Server Error")
			}
		}()
		h := rw.Header()
		h.Set("X-Request-ID", id)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if c.Environment == "production" {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := false
		for _, o := range c.CORSOrigins {
			if origin == o {
				allowed = true
				break
			}
		}
		if allowed {
			h.Set("Access-Control-Allow-Origin", origin)
			if c.CORSCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			h.Set("Access-Control-Expose-Headers", "X-Request-ID, RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, Retry-After")
		}
		if o.rateLimitExempt == nil || !o.rateLimitExempt(r) {
			remaining, reset, full := limiter.take(clientIP(r, c.TrustProxy), start)
			h.Set("RateLimit-Limit", strconv.Itoa(c.RateLimitMax))
			h.Set("RateLimit-Remaining", strconv.Itoa(max(0, remaining)))
			h.Set("RateLimit-Reset", strconv.Itoa(reset))
			if full {
				h.Set("Retry-After", strconv.Itoa(reset))
				Failure(rw, 503, "SERVICE_UNAVAILABLE", "Rate limiter capacity reached")
				return
			}
			if remaining < 0 {
				h.Set("Retry-After", strconv.Itoa(reset))
				Failure(rw, 429, "RATE_LIMIT_EXCEEDED", "Too many requests")
				return
			}
		}
		if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			method := r.Header.Get("Access-Control-Request-Method")
			if !allowed || !contains([]string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, method) {
				Failure(rw, 403, "CORS_FORBIDDEN", "Cross-origin request not allowed")
				return
			}
			for _, name := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				name = strings.ToLower(strings.TrimSpace(name))
				if name != "" && !contains([]string{"content-type", "authorization", "x-request-id"}, name) {
					Failure(rw, 403, "CORS_FORBIDDEN", "Cross-origin header not allowed")
					return
				}
			}
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			h.Set("Access-Control-Max-Age", "600")
			rw.WriteHeader(204)
			return
		}
		if r.ContentLength > c.BodyLimit {
			Failure(rw, 413, "PAYLOAD_TOO_LARGE", "Request body too large")
			return
		}
		r.Body = http.MaxBytesReader(rw, r.Body, c.BodyLimit)
		next.ServeHTTP(rw, r)
	})
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func clientIP(r *http.Request, trust bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trust {
		candidate := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if ip, err := netip.ParseAddr(candidate); err == nil {
			return ip.Unmap().String()
		}
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return "unknown"
}

type rateLimiter struct {
	mu            sync.Mutex
	counts        map[string]int
	max, capacity int
	window        time.Duration
	expires       time.Time
}

func (l *rateLimiter) take(key string, now time.Time) (remaining, reset int, full bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.expires) {
		clear(l.counts)
		l.expires = now.Add(l.window)
	}
	reset = max(1, int(math.Ceil(l.expires.Sub(now).Seconds())))
	count, exists := l.counts[key]
	if !exists && len(l.counts) >= l.capacity {
		return 0, reset, true
	}
	if count >= l.max {
		return -1, reset, false
	}
	l.counts[key] = count + 1
	return l.max - count - 1, reset, false
}
