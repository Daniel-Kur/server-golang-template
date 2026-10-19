package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticationAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		headers   []string
		principal Principal
		err       error
		status    int
		challenge string
		calls     int
	}{
		{name: "missing", status: 401, challenge: "Bearer"},
		{name: "other scheme", headers: []string{"Basic abc"}, status: 401, challenge: "Bearer"},
		{name: "empty bearer", headers: []string{"Bearer"}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "multiple", headers: []string{"Bearer a", "Bearer b"}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "comma", headers: []string{"Bearer a,b"}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "space in token", headers: []string{"Bearer a b"}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "oversized", headers: []string{"Bearer " + strings.Repeat("a", 8193)}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "bad padding", headers: []string{"Bearer a=b"}, status: 400, challenge: `Bearer error="invalid_request"`},
		{name: "invalid", headers: []string{"Bearer token"}, err: fmt.Errorf("wrapped: %w", ErrInvalidToken), status: 401, challenge: `Bearer error="invalid_token"`, calls: 1},
		{name: "provider failure", headers: []string{"Bearer token"}, err: errors.New("SECRET"), status: 503, calls: 1},
		{name: "empty subject", headers: []string{"Bearer token"}, status: 503, calls: 1},
		{name: "forbidden", headers: []string{"Bearer token"}, principal: Principal{Subject: "user"}, status: 403, challenge: `Bearer error="insufficient_scope"`, calls: 1},
		{name: "all required", headers: []string{"Bearer token"}, principal: Principal{Subject: "user", Permissions: []string{"read"}}, status: 403, challenge: `Bearer error="insufficient_scope"`, calls: 1},
		{name: "accepted", headers: []string{"bEaReR   abc._~+/-=="}, principal: Principal{Subject: "user", Permissions: []string{"read", "write"}}, status: 200, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, reached := 0, false
			verifier := VerifierFunc(func(ctx context.Context, token string) (Principal, error) {
				calls++
				if ctx.Err() != context.Canceled {
					t.Error("request context not propagated")
				}
				if token == "" || strings.Contains(token, " ") {
					t.Error("bad token passed to verifier")
				}
				return tc.principal, tc.err
			})
			handler := Authenticate(verifier)(RequirePermissions("read", "write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				p, ok := FromContext(r.Context())
				if !ok || p.Subject != "user" {
					t.Fatal("missing identity")
				}
				p.Permissions[0] = "mutated"
				again, _ := FromContext(r.Context())
				if again.Permissions[0] != "read" {
					t.Fatal("mutable context identity")
				}
				tc.principal.Permissions[0] = "provider-mutated"
				again, _ = FromContext(r.Context())
				if again.Permissions[0] != "read" {
					t.Fatal("provider alias retained")
				}
				w.WriteHeader(http.StatusOK)
			})))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := httptest.NewRequest(http.MethodGet, "/?access_token=ignored", nil).WithContext(ctx)
			for _, v := range tc.headers {
				r.Header.Add("Authorization", v)
			}
			r.AddCookie(&http.Cookie{Name: "access_token", Value: "ignored"})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("WWW-Authenticate") != tc.challenge || calls != tc.calls || reached != (tc.status == 200) {
				t.Fatalf("status=%d challenge=%q calls=%d reached=%v body=%s", w.Code, w.Header().Get("WWW-Authenticate"), calls, reached, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "SECRET") {
				t.Fatal("unsafe response")
			}
		})
	}
}

func TestAuthorizationRequiresIdentity(t *testing.T) {
	w := httptest.NewRecorder()
	RequirePermissions()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached handler") })).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
}

func TestAuthenticationOnly(t *testing.T) {
	verifier := VerifierFunc(func(context.Context, string) (Principal, error) { return Principal{Subject: "user"}, nil })
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer token")
	Authenticate(verifier)(RequirePermissions()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatal(w.Code)
	}
}
