// Package db owns the schema: goose migrations for Finchie's tables and River's own.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrSchemaNewer means the database was migrated by a newer backend than this one.
var ErrSchemaNewer = errors.New("database schema is newer than this backend")

// Migrate brings the schema up to date. It runs at startup, so a new image never meets an old schema.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := MigrateTo(ctx, pool, goose.MaxVersion); err != nil {
		return err
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrations: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("apply river migrations: %w", err)
	}
	return nil
}

// MigrateTo brings Finchie's schema up to a version, for a test of what a migration does to the rows an
// older schema held. Migrate is the only way the backend migrates.
func MigrateTo(ctx context.Context, pool *pgxpool.Pool, version int64) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }() // only wraps the pool, which the caller owns
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, files)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	// An older image started on a newer schema would run its queries against tables it does not know,
	// and its migrations would do nothing: refuse to start instead (ADR 0005's originals stay; a newer
	// image, or restoring the database from before the upgrade, is the way back).
	current, newest, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("schema version: %w", err)
	}
	if current > newest {
		return fmt.Errorf("%w: the database is at version %d, this backend knows up to %d", ErrSchemaNewer, current, newest)
	}
	if _, err := provider.UpTo(ctx, version); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
