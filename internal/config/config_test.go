package config

import "testing"

func TestLoad(t *testing.T) {
	lookup := func(env map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	}
	c, err := Load(lookup(nil))
	if err != nil || c.Address() != "0.0.0.0:3000" || !c.OpenAPI || len(c.CORSOrigins) != 1 {
		t.Fatal(c, err)
	}
	c, err = Load(lookup(map[string]string{"APP_ENV": "production", "HOST": "::1"}))
	if err != nil || c.OpenAPI || len(c.CORSOrigins) != 0 || c.Address() != "[::1]:3000" {
		t.Fatal(c, err)
	}
	for k, v := range map[string]string{"APP_ENV": "bad", "HOST": " ", "PORT": "65536", "LOG_LEVEL": "trace", "OPENAPI_ENABLED": "yes", "CORS_CREDENTIALS": "1", "TRUST_PROXY": "", "BODY_LIMIT_BYTES": "0", "RATE_LIMIT_MAX": "-1", "RATE_LIMIT_CLIENTS": "0", "RATE_LIMIT_WINDOW_MS": "9999999999999999999", "SHUTDOWN_TIMEOUT_MS": "0", "CORS_ORIGINS": "https://example.com/path"} {
		t.Run(k, func(t *testing.T) {
			if _, err := Load(lookup(map[string]string{k: v})); err == nil {
				t.Fatal("expected invalid configuration")
			}
		})
	}
	for _, origin := range []string{"*", "https://user:pass@example.com", "ftp://example.com", "http://", "https://example.com?", "https://example.com#", "https://*.example.com"} {
		if _, err := Load(lookup(map[string]string{"CORS_ORIGINS": origin})); err == nil {
			t.Errorf("accepted %q", origin)
		}
	}
	if _, err := Load(lookup(map[string]string{"CORS_ORIGINS": "https://example.com, http://localhost:8080", "LOG_LEVEL": "debug", "TRUST_PROXY": "true"})); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidOriginPorts(t *testing.T) {
	for _, origin := range []string{"https://example.com:0", "https://example.com:65536", "https://example.com:"} {
		_, err := Load(func(key string) (string, bool) { return origin, key == "CORS_ORIGINS" })
		if err == nil {
			t.Fatalf("accepted %s", origin)
		}
	}
}
