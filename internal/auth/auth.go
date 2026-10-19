// Package auth provides bearer authentication and explicit permission checks.
// Token validation belongs to an injected, concurrency-safe Verifier.
package auth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"example.com/server-golang-template/internal/httpapi"
)

// ErrInvalidToken identifies an invalid, expired, or revoked credential.
// Other verifier errors mean verification is unavailable, not successful.
var ErrInvalidToken = errors.New("invalid bearer token")

// Principal contains only verified identity and trusted application permissions.
type Principal struct {
	Subject     string
	Permissions []string
}

// Verifier must validate credentials, respect cancellation and outbound deadlines,
// and be safe for concurrent use. Never return claims from an unverified token.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

// VerifierFunc adapts a verification function to Verifier.
type VerifierFunc func(context.Context, string) (Principal, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (Principal, error) {
	return f(ctx, token)
}

type principalKey struct{}

// FromContext returns a defensive copy of the authenticated identity.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	p.Permissions = slices.Clone(p.Permissions)
	return p, ok
}

var bearerToken = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)

// Authenticate accepts only one Authorization: Bearer header. Cookies and query
// parameters are never credentials. A nil verifier fails closed with 503.
func Authenticate(verifier Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			values := r.Header.Values("Authorization")
			if len(values) == 0 {
				unauthorized(w)
				return
			}
			if len(values) != 1 {
				malformed(w)
				return
			}
			scheme, token, found := strings.Cut(values[0], " ")
			if !strings.EqualFold(scheme, "Bearer") {
				unauthorized(w)
				return
			}
			token = strings.TrimLeft(token, " ")
			if !found || len(token) > 8192 || !bearerToken.MatchString(token) {
				malformed(w)
				return
			}
			if verifier == nil {
				unavailable(w)
				return
			}
			p, err := verifier.Verify(r.Context(), token)
			if errors.Is(err, ErrInvalidToken) {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				httpapi.Failure(w, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid or expired access token")
				return
			}
			if err != nil || strings.TrimSpace(p.Subject) == "" {
				unavailable(w)
				return
			}
			p.Permissions = slices.Clone(p.Permissions)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		})
	}
}

// RequirePermissions requires an authenticated principal and every exact,
// case-sensitive permission. An empty list means authentication only; an empty
// permission name always denies access. No wildcard or implicit admin bypass exists.
func RequirePermissions(permissions ...string) func(http.Handler) http.Handler {
	required := slices.Clone(permissions)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			p, ok := FromContext(r.Context())
			if !ok {
				unauthorized(w)
				return
			}
			for _, permission := range required {
				if permission == "" || !slices.Contains(p.Permissions, permission) {
					w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
					httpapi.Failure(w, http.StatusForbidden, "FORBIDDEN", "Required permission is missing")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Protect applies authentication before authorization and declares the same
// policy in OpenAPI. Prefer this helper for application routes.
func Protect(route httpapi.Route, verifier Verifier, permissions ...string) httpapi.Route {
	route.Authenticated = true
	route.Permissions = slices.Clone(permissions)
	route.Errors = slices.Clone(route.Errors)
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable} {
		if !slices.Contains(route.Errors, status) {
			route.Errors = append(route.Errors, status)
		}
	}
	route.Handler = Authenticate(verifier)(RequirePermissions(permissions...)(route.Handler)).ServeHTTP
	return route
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpapi.Failure(w, http.StatusUnauthorized, "UNAUTHORIZED", "Bearer authentication is required")
}
func malformed(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_request"`)
	httpapi.Failure(w, http.StatusBadRequest, "INVALID_AUTH_REQUEST", "Malformed Authorization header")
}
func unavailable(w http.ResponseWriter) {
	httpapi.Failure(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "Authentication service is unavailable")
}
