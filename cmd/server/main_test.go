package main

import (
	"strings"
	"testing"
)

func TestRunRejectsConfigurationBeforeListening(t *testing.T) {
	t.Setenv("APP_ENV", "invalid")
	if err := run(); err == nil || !strings.Contains(err.Error(), "configuration: APP_ENV") {
		t.Fatalf("got %v", err)
	}
}
