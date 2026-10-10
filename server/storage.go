package server

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseConnectTimeout = 5 * time.Second
	databaseMaxConns       = 10
	databaseMaxIdleTime    = 5 * time.Minute
	databaseConnLifetime   = time.Hour
)

// NewPool opens a pgx connection pool for the given DATABASE_URL. Used by the
// start command so the server fails fast on boot if the database is
// unreachable; callers own closing the pool.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	// Sane defaults for a link service: a handful of conns, bounded lifetimes.
	cfg.MaxConns = databaseMaxConns
	cfg.MinIdleConns = 0
	cfg.MaxConnIdleTime = databaseMaxIdleTime
	cfg.MaxConnLifetime = databaseConnLifetime

	connectCtx, cancel := context.WithTimeout(ctx, databaseConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("opening pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return pool, nil
}
