// Package migrate runs Goose migrations against a pgx pool.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrator applies and rolls back migrations from one file system.
type Migrator struct {
	provider *goose.Provider
}

// Result describes one migration that was applied or rolled back.
type Result struct {
	Version int64
	Source  string
}

// Status describes one known migration.
type Status struct {
	Version   int64
	Source    string
	Applied   bool
	AppliedAt time.Time
}

// New builds a Migrator. Migration files must sit at the root of fsys.
// The sql.DB it creates shares pool's connections; the pool owns their lifetime.
func New(pool *pgxpool.Pool, fsys fs.FS) (*Migrator, error) {
	provider, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), fsys)
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return &Migrator{provider: provider}, nil
}

// Up applies every pending migration and returns the ones it ran.
func (m *Migrator) Up(ctx context.Context) ([]Result, error) {
	results, err := m.provider.Up(ctx)
	out := toResults(results)
	if err != nil {
		return out, fmt.Errorf("migrate up: %w", err)
	}
	return out, nil
}

// Down rolls back the most recent migration (one step).
func (m *Migrator) Down(ctx context.Context) (Result, error) {
	res, err := m.provider.Down(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("migrate down: %w", err)
	}
	return toResult(res), nil
}

// Status lists every known migration in version order.
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	list, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate status: %w", err)
	}
	out := make([]Status, len(list))
	for i, s := range list {
		out[i] = Status{
			Version:   s.Source.Version,
			Source:    s.Source.Path,
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt,
		}
	}
	return out, nil
}

func toResults(in []*goose.MigrationResult) []Result {
	out := make([]Result, 0, len(in))
	for _, r := range in {
		out = append(out, toResult(r))
	}
	return out
}

func toResult(r *goose.MigrationResult) Result {
	return Result{Version: r.Source.Version, Source: r.Source.Path}
}
