package server

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/server-golang-template/internal/auth"
	"net/http"
	"strings"
	"testing"
)

func TestProtectedAccountFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"unconfigured", "Bearer example-token", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(New(testConfig(t, nil), quiet()), http.MethodGet, "/account/me", "", map[string]string{"Authorization": tc.token})
			if w.Code != tc.status {
				t.Fatalf("want %d, got %d: %s", tc.status, w.Code, w.Body)
			}
		})
	}
}

func TestProtectedAccountContract(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		err         error
		status      int
	}{
		{name: "accepted", permissions: []string{"profile:read"}, status: 200},
		{name: "denied", status: 403},
		{name: "invalid", err: auth.ErrInvalidToken, status: 401},
		{name: "unavailable", err: errors.New("provider secret"), status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New(testConfig(t, nil), quiet(), WithVerifier(auth.VerifierFunc(func(context.Context, string) (auth.Principal, error) {
				return auth.Principal{Subject: "user-123", Permissions: tc.permissions}, tc.err
			})))
			w := request(app, http.MethodGet, "/account/me", "", map[string]string{"Authorization": "Bearer token"})
			if w.Code != tc.status || strings.Contains(w.Body.String(), "provider secret") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.status == 200 && w.Body.String() != "{\"success\":true,\"data\":{\"subject\":\"user-123\"}}\n" {
				t.Fatal(w.Body.String())
			}
			head := request(app, http.MethodHead, "/account/me", "", map[string]string{"Authorization": "Bearer token"})
			if head.Code != tc.status || head.Body.Len() != 0 {
				t.Fatal("HEAD contract")
			}
		})
	}
	app := New(testConfig(t, nil), quiet())
	specResponse := request(app, http.MethodGet, "/openapi/json", "", nil)
	var spec struct {
		Paths map[string]map[string]struct {
			Security    []map[string][]string      `json:"security"`
			Permissions []string                   `json:"x-required-permissions"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]map[string]string `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specResponse.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	op := spec.Paths["/account/me"]["get"]
	if len(op.Security) != 1 || len(op.Permissions) != 1 || op.Permissions[0] != "profile:read" {
		t.Fatal("missing auth metadata")
	}
	if scopes, ok := op.Security[0]["bearerAuth"]; !ok || len(scopes) != 0 {
		t.Fatal("HTTP bearer must not declare OAuth scopes")
	}
	for _, status := range []string{"200", "400", "401", "403", "413", "429", "500", "503"} {
		if op.Responses[status] == nil {
			t.Fatalf("missing %s response", status)
		}
	}
	if spec.Components.SecuritySchemes["bearerAuth"]["scheme"] != "bearer" || len(spec.Paths["/health"]["get"].Security) != 0 {
		t.Fatal("wrong security schemes")
	}
	preflight := request(app, http.MethodOptions, "/account/me", "", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "Authorization"})
	if preflight.Code != http.StatusNoContent {
		t.Fatal(preflight.Code)
	}
}

func TestDocsAuthenticationControls(t *testing.T) {
	w := request(New(testConfig(t, nil), quiet()), http.MethodGet, "/openapi/app.js", "", nil)
	for _, want := range []string{"Bearer access token", "x-required-permissions", "tokenInput.type = \"password\"", "init.headers.Authorization", "Bearer $TOKEN"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
}
