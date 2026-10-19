package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; w.WriteHeader(204) }))
	}()
	response := make(chan error, 1)
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			if closeErr := res.Body.Close(); closeErr != nil {
				t.Errorf("close response: %v", closeErr)
			}
		}
		response <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown did not drain: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown hung")
	}
}
func TestRunBindError(t *testing.T) {
	c := testConfig(t, nil)
	c.Host = "invalid host"
	if err := Run(context.Background(), c, quiet()); err == nil {
		t.Fatal("expected listen error")
	}
}
func TestServeClosedListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := serve(context.Background(), listener, testConfig(t, nil), http.NotFoundHandler()); err == nil {
		t.Fatal("expected serve error")
	}
}

func TestServeShutdownDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t, nil)
	c.ShutdownTimeout = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(finished) }))
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := http.Client{Timeout: time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			if closeErr := res.Body.Close(); closeErr != nil {
				t.Errorf("close response: %v", closeErr)
			}
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected shutdown deadline error")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown hung")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request context not canceled")
	}
	<-clientDone
}

func TestRunCanceledBeforeStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, testConfig(t, nil), quiet()); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
