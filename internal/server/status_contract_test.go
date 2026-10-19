package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHealthStatusContracts(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, contentType string
		status                                int
		code                                  string
		env                                   map[string]string
		prime                                 bool
		failRead                              bool
	}{
		{name: "liveness", method: http.MethodGet, path: "/health", status: http.StatusOK},
		{name: "uptime", method: http.MethodGet, path: "/health/uptime", status: http.StatusOK},
		{name: "echo does not create", method: http.MethodPost, path: "/health", body: `{"source":"test"}`, contentType: "application/json", status: http.StatusOK},
		{name: "invalid JSON", method: http.MethodPost, path: "/health", body: `{`, contentType: "application/json", status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "invalid field", method: http.MethodPost, path: "/health", body: `{"source":1}`, contentType: "application/json", status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "media type", method: http.MethodPost, path: "/health", body: `{}`, status: http.StatusUnsupportedMediaType, code: "UNSUPPORTED_MEDIA_TYPE"},
		{name: "oversize", method: http.MethodPost, path: "/health", body: `{"source":"test"}`, contentType: "application/json", status: http.StatusRequestEntityTooLarge, code: "PAYLOAD_TOO_LARGE", env: map[string]string{"BODY_LIMIT_BYTES": "2"}},
		{name: "read failure", method: http.MethodPost, path: "/health", contentType: "application/json", status: http.StatusBadRequest, code: "BAD_REQUEST", failRead: true},
		{name: "quota exceeded", method: http.MethodGet, path: "/health/uptime", status: http.StatusTooManyRequests, code: "RATE_LIMIT_EXCEEDED", env: map[string]string{"RATE_LIMIT_MAX": "1"}, prime: true},
		{name: "limiter capacity", method: http.MethodGet, path: "/health/uptime", status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE", env: map[string]string{"RATE_LIMIT_CLIENTS": "1"}, prime: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Read metadata from a separate handler so documentation requests don't use quotas.
			docs := request(New(testConfig(t, tc.env), quiet()), http.MethodGet, "/openapi/json", "", nil)
			var spec struct {
				Paths map[string]map[string]struct {
					Responses map[string]json.RawMessage `json:"responses"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(docs.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			app := New(testConfig(t, tc.env), quiet())
			if tc.prime {
				first := httptest.NewRequest(http.MethodGet, "/health/uptime", nil)
				first.RemoteAddr = "192.0.2.1:1234"
				app.ServeHTTP(httptest.NewRecorder(), first)
			}
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.RemoteAddr = "192.0.2.1:1234"
			if tc.status == http.StatusServiceUnavailable {
				r.RemoteAddr = "192.0.2.2:1234"
			}
			if tc.failRead {
				r.Body = brokenBody{}
				r.ContentLength = -1
			}
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("want %d, got %d: %s", tc.status, w.Code, w.Body)
			}
			var envelope struct {
				Success bool `json:"success"`
				Error   struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Success != (tc.status < 400) || envelope.Error.Code != tc.code {
				t.Fatalf("status and envelope disagree: %s", w.Body)
			}
			if spec.Paths[tc.path][strings.ToLower(tc.method)].Responses[strconv.Itoa(w.Code)] == nil {
				t.Fatalf("HTTP %d missing from OpenAPI", w.Code)
			}
		})
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("read failure") }
func (brokenBody) Close() error             { return nil }

func TestEchoOpenAPIDescribesNonPersistentOperation(t *testing.T) {
	w := request(New(testConfig(t, nil), quiet()), http.MethodGet, "/openapi/json", "", nil)
	var spec struct {
		Paths map[string]map[string]struct {
			Description string                     `json:"description"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	echo := spec.Paths["/health"]["post"]
	if !strings.Contains(echo.Description, "does not create or persist") {
		t.Fatal("OpenAPI must explain why the diagnostic POST returns 200")
	}
	if echo.Responses["200"] == nil || echo.Responses["201"] != nil || echo.Responses["409"] != nil {
		t.Fatal("diagnostic endpoint must not promise creation or database conflict responses")
	}
}
