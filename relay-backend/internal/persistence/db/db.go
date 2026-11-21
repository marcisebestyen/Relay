// Package db opens the Postgres connection pool.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

const (
	maxConns          = 10
	minConns          = 1
	maxConnLifetime   = 30 * time.Minute
	maxConnIdleTime   = 5 * time.Minute
	healthCheckPeriod = time.Minute
)

// Open builds a pool from cfg and pings it. Errors never include the password.
func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, errors.New("database URL is empty")
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// pgx masks the password in parse errors.
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolCfg.MaxConns = maxConns
	poolCfg.MinConns = minConns
	poolCfg.MaxConnLifetime = maxConnLifetime
	poolCfg.MaxConnIdleTime = maxConnIdleTime
	poolCfg.HealthCheckPeriod = healthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Close closes pool but gives up waiting when ctx ends. Pool.Close blocks until
// every connection is returned, so a stuck query could hang shutdown.
func Close(ctx context.Context, pool *pgxpool.Pool) error {
	done := make(chan struct{})
	go func() {
		pool.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("close database pool: %w", ctx.Err())
	}
}
