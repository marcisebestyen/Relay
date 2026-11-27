package workers

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// HeartbeatLoop beats on every Interval tick until its context ends. A failed
// beat is logged and retried on the next tick, so a short DB outage does not
// stop the worker. OnTick hooks run on every tick, after the beat. Lease
// renewal hooks go here.
type HeartbeatLoop struct {
	Interval time.Duration
	// Now gives the time for each beat. Nil means time.Now.
	Now func() time.Time
	// Logger receives failed beats. Nil means slog.Default.
	Logger *slog.Logger
	// Beat records one heartbeat at the given time.
	Beat func(ctx context.Context, at time.Time) error
	// OnTick hooks run on every tick, even when the beat failed.
	OnTick []func(ctx context.Context)
}

// Run blocks until ctx ends. It returns an error only when Interval is not
// positive or Beat is nil.
func (l HeartbeatLoop) Run(ctx context.Context) error {
	if l.Interval <= 0 {
		return errors.New("heartbeat interval must be > 0")
	}
	if l.Beat == nil {
		return errors.New("heartbeat loop needs a Beat function")
	}
	ticker := time.NewTicker(l.Interval)
	defer ticker.Stop()
	l.run(ctx, ticker.C)
	return nil
}

func (l HeartbeatLoop) run(ctx context.Context, ticks <-chan time.Time) {
	now := l.Now
	if now == nil {
		now = time.Now
	}
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		if err := l.Beat(ctx, now().UTC()); err != nil && ctx.Err() == nil {
			logger.Warn("heartbeat failed, will retry on next tick", "error", err)
		}
		for _, hook := range l.OnTick {
			hook(ctx)
		}
	}
}
