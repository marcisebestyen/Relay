package workers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var loopNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func fixedNow() time.Time { return loopNow }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)) }

func runAsync(ctx context.Context, l HeartbeatLoop, ticks <-chan time.Time) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		l.run(ctx, ticks)
		close(done)
	}()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return after context was cancelled")
	}
}

func TestLoopBeatsOncePerTick(t *testing.T) {
	ticks := make(chan time.Time)
	beats := make(chan time.Time, 10)
	l := HeartbeatLoop{
		Now:    fixedNow,
		Logger: discardLogger(),
		Beat: func(_ context.Context, at time.Time) error {
			beats <- at
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ctx, l, ticks)

	for range 3 {
		ticks <- loopNow
		if at := <-beats; !at.Equal(loopNow) {
			t.Fatalf("beat at %s, want %s", at, loopNow)
		}
	}
	cancel()
	waitDone(t, done)
}

func TestLoopKeepsBeatingAfterFailedBeat(t *testing.T) {
	ticks := make(chan time.Time)
	results := make(chan error, 10)
	calls := 0
	var logs bytes.Buffer
	l := HeartbeatLoop{
		Now:    fixedNow,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Beat: func(_ context.Context, _ time.Time) error {
			calls++
			var err error
			if calls == 1 {
				err = errors.New("db down")
			}
			results <- err
			return err
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ctx, l, ticks)

	ticks <- loopNow
	if err := <-results; err == nil {
		t.Fatal("first beat: want simulated error")
	}
	ticks <- loopNow
	if err := <-results; err != nil {
		t.Fatalf("second beat: %v", err)
	}
	cancel()
	waitDone(t, done)

	if calls != 2 {
		t.Fatalf("beat calls = %d, want 2", calls)
	}
	if !strings.Contains(logs.String(), "heartbeat failed") {
		t.Fatalf("failed beat not logged: %s", logs.String())
	}
}

func TestLoopRunsHooksOnEveryTickEvenWhenBeatFails(t *testing.T) {
	ticks := make(chan time.Time)
	hookRuns := make(chan struct{}, 10)
	l := HeartbeatLoop{
		Now:    fixedNow,
		Logger: discardLogger(),
		Beat:   func(_ context.Context, _ time.Time) error { return errors.New("db down") },
		OnTick: []func(context.Context){func(context.Context) { hookRuns <- struct{}{} }},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runAsync(ctx, l, ticks)

	for range 2 {
		ticks <- loopNow
		<-hookRuns
	}
	cancel()
	waitDone(t, done)
}

func TestLoopReturnsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	l := HeartbeatLoop{
		Now:    fixedNow,
		Logger: discardLogger(),
		Beat:   func(context.Context, time.Time) error { return nil },
	}
	done := runAsync(ctx, l, make(chan time.Time))
	cancel()
	waitDone(t, done)
}

func TestRunRejectsNonPositiveInterval(t *testing.T) {
	l := HeartbeatLoop{Beat: func(context.Context, time.Time) error { return nil }}
	if err := l.Run(context.Background()); err == nil {
		t.Fatal("Run with zero interval: want error")
	}
}
