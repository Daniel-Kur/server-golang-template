// Package config loads and validates process configuration at startup.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment, Host                    string
	Port                                 int
	LogLevel                             slog.Level
	OpenAPI, CORSCredentials, TrustProxy bool
	CORSOrigins                          []string
	BodyLimit                            int64
	RateLimitMax, RateLimitClients       int
	RateLimitWindow, ShutdownTimeout     time.Duration
}

// Load accepts an environment lookup so tests never mutate process globals.
// Empty values are invalid except CORS_ORIGINS, where empty disables CORS.
func Load(lookup func(string) (string, bool)) (Config, error) {
	c := Config{Environment: "development", Host: "0.0.0.0", Port: 3000, CORSCredentials: true,
		BodyLimit: 1 << 20, RateLimitMax: 100, RateLimitClients: 10000, RateLimitWindow: time.Minute, ShutdownTimeout: 10 * time.Second}
	get := func(k, fallback string) string {
		if v, ok := lookup(k); ok {
			return v
		}
		return fallback
	}
	c.Environment = get("APP_ENV", c.Environment)
	switch c.Environment {
	case "development", "test", "production":
	default:
		return c, fmt.Errorf("APP_ENV must be development, test, or production")
	}
	c.Host = get("HOST", c.Host)
	if strings.TrimSpace(c.Host) == "" {
		return c, fmt.Errorf("HOST must not be empty")
	}
	levels := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}
	var ok bool
	c.LogLevel, ok = levels[get("LOG_LEVEL", "info")]
	if !ok {
		return c, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	for _, item := range []struct {
		name     string
		target   *bool
		fallback bool
	}{
		{"OPENAPI_ENABLED", &c.OpenAPI, c.Environment != "production"}, {"CORS_CREDENTIALS", &c.CORSCredentials, true}, {"TRUST_PROXY", &c.TrustProxy, false},
	} {
		v := get(item.name, strconv.FormatBool(item.fallback))
		if v != "true" && v != "false" {
			return c, fmt.Errorf("%s must be true or false", item.name)
		}
		*item.target = v == "true"
	}
	for _, item := range []struct {
		name   string
		target *int
		max    int
	}{
		{"PORT", &c.Port, 65535}, {"RATE_LIMIT_MAX", &c.RateLimitMax, 1000000}, {"RATE_LIMIT_CLIENTS", &c.RateLimitClients, 1000000},
	} {
		v, err := strconv.Atoi(get(item.name, strconv.Itoa(*item.target)))
		if err != nil || v < 1 || v > item.max {
			return c, fmt.Errorf("%s must be an integer between 1 and %d", item.name, item.max)
		}
		*item.target = v
	}
	body, err := strconv.ParseInt(get("BODY_LIMIT_BYTES", strconv.FormatInt(c.BodyLimit, 10)), 10, 64)
	if err != nil || body < 1 || body > 1<<30 {
		return c, fmt.Errorf("BODY_LIMIT_BYTES must be between 1 and 1073741824")
	}
	c.BodyLimit = body
	for _, item := range []struct {
		name   string
		target *time.Duration
	}{{"RATE_LIMIT_WINDOW_MS", &c.RateLimitWindow}, {"SHUTDOWN_TIMEOUT_MS", &c.ShutdownTimeout}} {
		v, err := strconv.ParseInt(get(item.name, strconv.FormatInt(item.target.Milliseconds(), 10)), 10, 64)
		if err != nil || v < 1 || v > 86400000 {
			return c, fmt.Errorf("%s must be milliseconds between 1 and 86400000", item.name)
		}
		*item.target = time.Duration(v) * time.Millisecond
	}
	defaults := "http://localhost:3000"
	if c.Environment == "production" {
		defaults = ""
	}
	for _, origin := range strings.Split(get("CORS_ORIGINS", defaults), ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(origin, "*#") {
			return c, fmt.Errorf("CORS_ORIGINS must contain exact HTTP(S) origins without paths")
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return c, fmt.Errorf("CORS_ORIGINS contains an invalid port")
			}
		} else if strings.HasSuffix(u.Host, ":") {
			return c, fmt.Errorf("CORS_ORIGINS contains an empty port")
		}
		c.CORSOrigins = append(c.CORSOrigins, origin)
	}
	return c, nil
}

func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
