package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLivenessRequest(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/health", true},
		{http.MethodHead, "/health", true},
		{http.MethodGet, "/health?probe=1", true},
		{http.MethodPost, "/health", false},
		{http.MethodOptions, "/health", false},
		{http.MethodGet, "/health/uptime", false},
		{http.MethodGet, "/health/", false},
		{http.MethodGet, "/other", false},
	} {
		if got := IsLivenessRequest(httptest.NewRequest(tc.method, tc.path, nil)); got != tc.want {
			t.Errorf("%s %s: got %v", tc.method, tc.path, got)
		}
	}
}
