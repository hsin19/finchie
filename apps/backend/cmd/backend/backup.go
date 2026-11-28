package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hsin19/finchie/apps/backend/internal/backup"
	"github.com/hsin19/finchie/apps/backend/internal/db"
)

func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	var snapshotDir, blobsDir, databaseURL string
	fs.StringVar(&snapshotDir, "out", "", "directory to write the snapshot into")
	fs.StringVar(&blobsDir, "blobs", "", "directory for content-addressed document originals")
	fs.StringVar(&databaseURL, "database-url", os.Getenv("BACKEND_DATABASE_URL"), "Postgres connection URL (default BACKEND_DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if snapshotDir == "" || blobsDir == "" {
		return errors.New("usage: backend backup --out <snapshot-dir> --blobs <blobs-dir>")
	}
	if databaseURL == "" {
		return errors.New("database URL is required (set BACKEND_DATABASE_URL or --database-url)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := backup.Backup(ctx, pool, snapshotDir, blobsDir)
	if err != nil {
		return err
	}
	fmt.Printf("backup complete: schema=%d tables=%d rows=%d blobs=%d new_blobs=%d repaired_blobs=%d\n",
		res.SchemaVersion, res.Tables, res.Rows, res.BlobsTotal, res.BlobsWritten, res.BlobsRepaired)
	return nil
}

func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	var snapshotDir, blobsDir, databaseURL string
	fs.StringVar(&snapshotDir, "in", "", "snapshot directory to restore from")
	fs.StringVar(&blobsDir, "blobs", "", "directory containing document originals")
	fs.StringVar(&databaseURL, "database-url", os.Getenv("BACKEND_DATABASE_URL"), "Postgres connection URL (default BACKEND_DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if snapshotDir == "" || blobsDir == "" {
		return errors.New("usage: backend restore --in <snapshot-dir> --blobs <blobs-dir>")
	}
	if databaseURL == "" {
		return errors.New("database URL is required (set BACKEND_DATABASE_URL or --database-url)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := backup.Restore(ctx, pool, snapshotDir, blobsDir)
	if err != nil {
		return err
	}
	fmt.Printf("restore complete: schema=%d tables=%d rows=%d blobs=%d\n",
		res.SchemaVersion, res.Tables, res.Rows, res.BlobsRestored)
	return nil
}
