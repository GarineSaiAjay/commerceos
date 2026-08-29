package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectRetries/connectRetryDelay bound how long NewPostgresPool waits
// for Postgres to become reachable before giving up. infra/docker-compose.yml's
// own health-gating (backend depends_on postgres: condition: service_healthy)
// is the first line of defense against starting the backend before
// Postgres can accept connections; this retry loop is the second,
// independent one -- e.g. for running the backend directly on the host
// against a Postgres container that's still starting, with no Compose
// health check in the picture at all. Previously a single failed Ping
// here was fatal (os.Exit(1) in main.go) with no retry whatsoever.
const (
	connectRetries    = 10
	connectRetryDelay = 2 * time.Second
)

func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	var pingErr error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		pingErr = pool.Ping(ctx)
		if pingErr == nil {
			return pool, nil
		}

		if attempt < connectRetries {
			fmt.Printf(
				"postgres not ready yet (attempt %d/%d): %v\n",
				attempt, connectRetries, pingErr,
			)

			select {
			case <-ctx.Done():
				pool.Close()
				return nil, ctx.Err()
			case <-time.After(connectRetryDelay):
			}
		}
	}

	pool.Close()
	return nil, fmt.Errorf("ping postgres after %d attempts: %w", connectRetries, pingErr)
}
