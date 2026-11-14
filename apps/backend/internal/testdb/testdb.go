// Package testdb starts a throwaway Postgres for integration tests. Tests are skipped when Docker
// is not available.
package testdb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/hsin19/finchie/apps/backend/internal/db"
)

// Same major version as the homelab.
const image = "postgres:18.6"

var (
	once    sync.Once
	pool    *pgxpool.Pool
	initErr error
	// The container's connection string, for databases of a test's own.
	dsn     string
	freshID atomic.Int64
)

// Pool returns a migrated database shared by the package's tests, emptied before each test.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	once.Do(func() { pool, initErr = start() })
	if initErr != nil {
		t.Fatalf("start postgres: %v", initErr)
	}
	ctx := t.Context()
	// Units outlive accounts, so they keep their ids; what the migrations seed (NT$, the owner's prices of
	// card points) is put back.
	if _, err := pool.Exec(ctx, `
		TRUNCATE documents, accounts, prices, exchange_cursors, river_job RESTART IDENTITY CASCADE;
		DELETE FROM units WHERE id NOT IN (SELECT id FROM test_seed_units);
		INSERT INTO prices SELECT * FROM test_seed_prices;`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	return pool
}

func start() (*pgxpool.Pool, error) {
	ctx := context.Background()
	// The container is removed by testcontainers' reaper when the test binary exits.
	container, err := postgres.Run(ctx, image,
		postgres.WithDatabase("finchie"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return nil, fmt.Errorf("run container: %w", err)
	}
	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("connection string: %w", err)
	}
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := waitForPing(ctx, p); err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, p); err != nil {
		return nil, err
	}
	if _, err := p.Exec(ctx, `
		CREATE TABLE test_seed_units AS SELECT id FROM units;
		CREATE TABLE test_seed_prices AS SELECT * FROM prices;`); err != nil {
		return nil, fmt.Errorf("keep the seeds: %w", err)
	}
	return p, nil
}

// Empty returns a database of the test's own with no schema at all, for a test that migrates it itself.
func Empty(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := Pool(t)
	name := fmt.Sprintf("empty_%d", freshID.Add(1))
	if _, err := shared.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	config.ConnConfig.Database = name
	p, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func waitForPing(ctx context.Context, p *pgxpool.Pool) error {
	var err error
	for range 50 {
		if err = p.Ping(ctx); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("ping: %w", err)
}
