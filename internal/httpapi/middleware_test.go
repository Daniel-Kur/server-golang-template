package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/server-golang-template/internal/config"
)

func TestLimiter(t *testing.T) {
	l := rateLimiter{counts: map[string]int{}, max: 1, capacity: 1, window: time.Second}
	now := time.Now()
	if n, _, full := l.take("a", now); n != 0 || full {
		t.Fatal(n, full)
	}
	if n, _, _ := l.take("a", now); n != -1 {
		t.Fatal(n)
	}
	if _, _, full := l.take("b", now); !full {
		t.Fatal("unbounded store")
	}
	if n, _, full := l.take("b", now.Add(time.Second)); n != 0 || full {
		t.Fatal("window did not reset")
	}
}
func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.2")
	if clientIP(r, false) != "192.0.2.1" || clientIP(r, true) != "198.51.100.1" {
		t.Fatal("proxy handling")
	}
	r.Header.Set("X-Forwarded-For", "bogus")
	if clientIP(r, true) != "192.0.2.1" {
		t.Fatal("invalid forwarded address")
	}
	r.RemoteAddr = "broken"
	if clientIP(r, false) != "unknown" {
		t.Fatal("invalid remote address")
	}
}
func TestRecoveryAndContext(t *testing.T) {
	c, _ := config.Load(func(string) (string, bool) { return "", false })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, committed := range []bool{false, true} {
		h := Middleware(c, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if RequestID(r.Context()) == "" {
				t.Error("missing context ID")
			}
			if committed {
				w.WriteHeader(200)
			}
			panic("secret")
		}))
		func() {
			defer func() {
				p := recover()
				err, _ := p.(error)
				if committed && !errors.Is(err, http.ErrAbortHandler) {
					t.Errorf("panic=%v", p)
				}
			}()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if !committed && w.Code != 500 {
				t.Fatal(w.Code)
			}
		}()
	}
	if RequestID(context.Background()) != "" {
		t.Fatal("unexpected ID")
	}
	c.RateLimitClients = 1
	h := Middleware(c, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for i, addr := range []string{"192.0.2.1:1", "192.0.2.2:2"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 1 && w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
}
func TestResponse(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, 200, make(chan int))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	e := &Error{Status: 400, Code: "BAD_REQUEST", Message: "Bad request"}
	if e.Error() != "Bad request" {
		t.Fatal(e)
	}
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	if rw.Unwrap() != rw.ResponseWriter {
		t.Fatal("unwrap")
	}
	_, _ = rw.Write([]byte("ok"))
	rw.WriteHeader(500)
	if rw.status != 200 {
		t.Fatal(rw.status)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func TestDecodeReadFailure(t *testing.T) {
	r := httptest.NewRequest("POST", "/", failedReader{})
	r.Header.Set("Content-Type", "application/json")
	_, err := (ObjectSchema{}).Decode(r)
	var public *Error
	if !errors.As(err, &public) || public.Status != 400 {
		t.Fatal(err)
	}
}

// Unlike httptest.ResponseRecorder, this writer tracks multiple 1xx responses.
type informationalWriter struct {
	header   http.Header
	statuses []int
}

func (w *informationalWriter) Header() http.Header         { return w.header }
func (w *informationalWriter) WriteHeader(status int)      { w.statuses = append(w.statuses, status) }
func (w *informationalWriter) Write(p []byte) (int, error) { return len(p), nil }
func TestInformationalAndFlush(t *testing.T) {
	base := &informationalWriter{header: make(http.Header)}
	w := &responseWriter{ResponseWriter: base}
	w.WriteHeader(103)
	if w.status != 0 {
		t.Fatal("informational response committed final status")
	}
	w.WriteHeader(201)
	if w.status != 201 || len(base.statuses) != 2 {
		t.Fatal(w.status, base.statuses)
	}
	recorder := httptest.NewRecorder()
	w = &responseWriter{ResponseWriter: recorder}
	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Fatal(err)
	}
	if w.status != 200 || !recorder.Flushed {
		t.Fatal("flush failed to track commitment")
	}
}

func TestPanicDiscardsEntityHeaders(t *testing.T) {
	c, _ := config.Load(func(string) (string, bool) { return "", false })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Middleware(c, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("ETag", `"stale"`)
		panic("private details")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || w.Header().Get("Content-Encoding") != "" || w.Header().Get("ETag") != "" {
		t.Fatal(w.Code, w.Header())
	}
	if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatal(w.Header(), w.Body)
	}
}

func TestErrorsRemainObservableAtErrorLevel(t *testing.T) {
	c, _ := config.Load(func(string) (string, bool) { return "", false })
	for _, abort := range []bool{false, true} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
		h := Middleware(c, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if abort {
				w.WriteHeader(200)
				panic("secret")
			}
			Failure(w, 500, "INTERNAL_SERVER_ERROR", "Internal Server Error")
		}))
		func() {
			defer func() {
				p := recover()
				err, _ := p.(error)
				if abort && !errors.Is(err, http.ErrAbortHandler) {
					t.Errorf("unexpected panic %v", p)
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		}()
		if !strings.Contains(logs.String(), `"level":"ERROR"`) || strings.Contains(logs.String(), "secret") {
			t.Fatal(logs.String())
		}
		if abort && !strings.Contains(logs.String(), `"aborted":true`) {
			t.Fatal(logs.String())
		}
	}
}

func TestMiddlewareHasNoImplicitEndpointExemptions(t *testing.T) {
	c, err := config.Load(func(key string) (string, bool) { return "1", key == "RATE_LIMIT_MAX" })
	if err != nil {
		t.Fatal(err)
	}
	h := Middleware(c, slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i, want := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		if w.Code != want {
			t.Fatalf("request %d: want %d, got %d", i, want, w.Code)
		}
	}
}

func TestInjectedRateLimitExemption(t *testing.T) {
	c, err := config.Load(func(key string) (string, bool) { return "1", key == "RATE_LIMIT_MAX" })
	if err != nil {
		t.Fatal(err)
	}
	c.BodyLimit = 1
	h := Middleware(c, slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), WithRateLimitExemption(func(r *http.Request) bool { return r.Method == http.MethodGet && r.URL.Path == "/probe" }))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/probe", "", http.StatusNoContent},
		{http.MethodGet, "/probe", "", http.StatusNoContent},
		{http.MethodGet, "/probe", "xx", http.StatusRequestEntityTooLarge},
		{http.MethodPost, "/probe", "", http.StatusNoContent},
		{http.MethodPost, "/probe", "", http.StatusTooManyRequests},
		{http.MethodGet, "/other", "", http.StatusTooManyRequests},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%s %s: want %d got %d", tc.method, tc.path, tc.status, w.Code)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("exemption bypassed security headers")
		}
	}
}
