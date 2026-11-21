package api_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestServeDrainsInFlightRequestOnCancel(t *testing.T) {
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})
	l := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- api.Serve(ctx, l, h, 5*time.Second, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}()

	got := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + l.Addr().String())
		if err != nil {
			got <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		got <- string(b)
	}()

	<-started
	cancel()
	if body := <-got; body != "done" {
		t.Errorf("in-flight response = %q", body)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve = %v", err)
	}
}

func TestServeReturnsErrorWhenDrainTimesOut(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	l := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- api.Serve(ctx, l, h, 50*time.Millisecond, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}()
	go func() { _, _ = http.Get("http://" + l.Addr().String()) }()

	<-started
	cancel()
	if err := <-served; err == nil {
		t.Error("expected shutdown timeout error")
	}
}
