package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"example.com/server-golang-template/internal/config"
)

func testConfig(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	c, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func request(app http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}
func TestAPI(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"health", "GET", "/health", "", 200}, {"head", "HEAD", "/health", "", 200}, {"uptime", "GET", "/health/uptime", "", 200},
		{"echo", "POST", "/health", `{"source":"test"}`, 200}, {"empty object", "POST", "/health", `{}`, 200},
		{"unknown field", "POST", "/health", `{"other":true}`, 422}, {"wrong type", "POST", "/health", `{"source":123}`, 422},
		{"null field", "POST", "/health", `{"source":null}`, 422}, {"null", "POST", "/health", `null`, 422}, {"array", "POST", "/health", `[]`, 422},
		{"empty string", "POST", "/health", `{"source":""}`, 422}, {"long string", "POST", "/health", `{"source":"` + strings.Repeat("a", 101) + `"}`, 422},
		{"unicode", "POST", "/health", `{"source":"` + strings.Repeat("界", 100) + `"}`, 200},
		{"duplicate", "POST", "/health", `{"source":"a","source":"b"}`, 422}, {"trailing", "POST", "/health", `{} {}`, 422},
		{"broken", "POST", "/health", `{"source":`, 422}, {"missing body", "POST", "/health", "", 422},
		{"missing", "GET", "/missing", "", 404}, {"method", "DELETE", "/health", "", 405},
		{"spec", "GET", "/openapi/json", "", 200}, {"docs", "GET", "/openapi", "", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := request(New(testConfig(t, nil), quiet()), tt.method, tt.path, tt.body, map[string]string{"Content-Type": "application/json", "X-Request-ID": "test-id"})
			if w.Code != tt.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if w.Header().Get("X-Request-ID") != "test-id" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing headers")
			}
			if tt.method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
				return
			}
			if tt.path == "/openapi" {
				if !strings.Contains(w.Body.String(), "/openapi/json") {
					t.Fatal("missing spec link")
				}
				return
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tt.status >= 400 {
				if body["success"] != false || body["error"] == nil {
					t.Fatal(body)
				}
			}
			if tt.name == "echo" {
				data := body["data"].(map[string]any)
				if data["from"] != "POST" || data["received"].(map[string]any)["source"] != "test" {
					t.Fatal(body)
				}
			}
			if tt.name == "health" && body["data"].(map[string]any)["status"] != "ok" {
				t.Fatal(body)
			}
			if tt.name == "uptime" && body["data"].(map[string]any)["uptimeMs"].(float64) < 0 {
				t.Fatal(body)
			}
			if tt.name == "spec" {
				paths := body["paths"].(map[string]any)
				if len(paths) != 3 || paths["/health/uptime"] == nil {
					t.Fatal(body)
				}
			}
		})
	}
}
func TestLimitsAndProduction(t *testing.T) {
	app := New(testConfig(t, map[string]string{"APP_ENV": "production", "BODY_LIMIT_BYTES": "16"}), quiet())
	for _, path := range []string{"/openapi", "/openapi/json"} {
		w := request(app, "GET", path, "", nil)
		if w.Code != 404 || w.Header().Get("Strict-Transport-Security") == "" {
			t.Fatal(w)
		}
	}
	for _, chunked := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/health", strings.NewReader(`{"source":"long body"}`))
		r.Header.Set("Content-Type", "application/json")
		if chunked {
			r.ContentLength = -1
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 413 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	w := request(app, "POST", "/health", `{}`, nil)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
}
func TestCORS(t *testing.T) {
	app := New(testConfig(t, nil), quiet())
	for _, tt := range []struct {
		origin, method, header string
		status                 int
	}{
		{"http://localhost:3000", "POST", "content-type, x-request-id", 204},
		{"https://evil.example", "POST", "content-type", 403},
		{"http://localhost:3000", "TRACE", "", 403},
		{"http://localhost:3000", "POST", "x-secret", 403},
	} {
		w := request(app, "OPTIONS", "/health", "", map[string]string{"Origin": tt.origin, "Access-Control-Request-Method": tt.method, "Access-Control-Request-Headers": tt.header})
		if w.Code != tt.status {
			t.Fatal(w.Code, w.Body)
		}
		if tt.status == 204 && w.Header().Get("Access-Control-Allow-Origin") != tt.origin {
			t.Fatal(w.Header())
		}
	}
	w := request(app, "GET", "/health", "", map[string]string{"Origin": "https://evil.example"})
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal(w.Header())
	}
}
func TestRateLimiting(t *testing.T) {
	app := New(testConfig(t, map[string]string{"RATE_LIMIT_MAX": "1"}), quiet())
	for i := 0; i < 3; i++ {
		if w := request(app, "GET", "/health", "", nil); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if w := request(app, "POST", "/health", `{}`, map[string]string{"Content-Type": "application/json"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := request(app, "GET", "/health/uptime", "", nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w)
	}
	// Liveness remains available after the client's quota has been exhausted.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := request(app, method, "/health", "", nil)
		if w.Code != http.StatusOK || w.Header().Get("RateLimit-Limit") != "" {
			t.Fatalf("%s liveness was rate limited: %d", method, w.Code)
		}
	}
}
func TestConcurrentRequests(t *testing.T) {
	app := New(testConfig(t, map[string]string{"RATE_LIMIT_MAX": "1000"}), quiet())
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := request(app, "GET", "/health/uptime", "", nil); w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	wg.Wait()
}
func TestLogsDoNotLeak(t *testing.T) {
	var logs bytes.Buffer
	app := New(testConfig(t, nil), slog.New(slog.NewJSONHandler(&logs, nil)))
	w := request(app, "POST", "/health?token=secret", `{"source":"secret"}`, map[string]string{"Content-Type": "application/json", "Authorization": "Bearer secret", "X-Request-ID": "bad id"})
	if strings.Contains(logs.String(), "secret") || w.Header().Get("X-Request-ID") == "bad id" {
		t.Fatal(logs.String())
	}
}

func TestInteractiveDocumentation(t *testing.T) {
	app := New(testConfig(t, nil), quiet())
	page := request(app, "GET", "/openapi", "", nil)
	if !strings.Contains(page.Body.String(), `src="/openapi/app.js"`) || !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal(page.Body, page.Header())
	}
	script := request(app, "GET", "/openapi/app.js", "", nil)
	if script.Code != 200 || !strings.Contains(script.Header().Get("Content-Type"), "javascript") || !strings.Contains(script.Body.String(), "fetch(url, init)") {
		t.Fatal(script.Code, script.Header())
	}
	prod := New(testConfig(t, map[string]string{"APP_ENV": "production"}), quiet())
	if w := request(prod, "GET", "/openapi/app.js", "", nil); w.Code != 404 {
		t.Fatal("docs assets exposed in production")
	}
	enabled := New(testConfig(t, map[string]string{"APP_ENV": "production", "OPENAPI_ENABLED": "true"}), quiet())
	if w := request(enabled, "GET", "/openapi", "", nil); w.Code != 200 {
		t.Fatal("explicit docs setting ignored")
	}
}

func TestHeadMatchesHealthRepresentation(t *testing.T) {
	app := New(testConfig(t, nil), quiet())
	get := request(app, "GET", "/health", "", nil)
	head := request(app, "HEAD", "/health", "", nil)
	if get.Header().Get("Content-Length") == "" || head.Header().Get("Content-Length") != get.Header().Get("Content-Length") || head.Body.Len() != 0 {
		t.Fatal(get.Header(), head.Header(), head.Body)
	}
}

func TestDocumentationStylesAndNavigation(t *testing.T) {
	app := New(testConfig(t, nil), quiet())
	page := request(app, "GET", "/openapi", "", nil)
	for _, marker := range []string{`href="/openapi/style.css"`, `aria-label="Search endpoints"`, `id="navigation"`, `id="operation-detail"`} {
		if !strings.Contains(page.Body.String(), marker) {
			t.Errorf("missing documentation control %s", marker)
		}
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "style-src 'self'") {
		t.Error("stylesheet blocked by CSP")
	}
	style := request(app, "GET", "/openapi/style.css", "", nil)
	if style.Code != 200 || !strings.Contains(style.Header().Get("Content-Type"), "text/css") {
		t.Errorf("stylesheet response: %d %v", style.Code, style.Header())
	}
	prod := New(testConfig(t, map[string]string{"APP_ENV": "production"}), quiet())
	if w := request(prod, "GET", "/openapi/style.css", "", nil); w.Code != 404 {
		t.Error("stylesheet should be disabled in production")
	}
}
