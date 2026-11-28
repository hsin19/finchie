// Package backup exports and restores Finchie's database with document originals separated into a
// content-addressed blob directory (ADR 0025). It reads and writes local directories only; encryption
// and offsite storage belong to the deployment.
package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db"
	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

const (
	// FormatVersion is the on-disk snapshot layout version recorded in manifest.json.
	FormatVersion = 1

	manifestFile  = "manifest.json"
	blobsListFile = "blobs.sha256"
	tablesDir     = "tables"

	// restorePendingTable marks a database that Restore migrated but has not finished loading. It is
	// created before migrating and dropped in the restore transaction, so it survives only a failed
	// restore, which may then be retried on the same database.
	restorePendingTable = "backup_restore_pending"
)

var (
	ErrDatabaseNotEmpty  = errors.New("target database is not empty")
	ErrSnapshotExists    = errors.New("snapshot directory already contains a backup")
	ErrBlobMissing       = errors.New("document blob missing")
	ErrBlobCorrupt       = errors.New("document blob hash mismatch")
	ErrUnsupportedFormat = errors.New("unsupported backup format version")
	ErrRestorePending    = errors.New("database holds an unfinished restore")
)

type Manifest struct {
	Format        int             `json:"format"`
	SchemaVersion int64           `json:"schema_version"`
	CreatedAt     time.Time       `json:"created_at"`
	Tables        []TableManifest `json:"tables"`
	Blobs         []string        `json:"blobs"`
}

type TableManifest struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    int64    `json:"rows"`
}

type BackupResult struct {
	SchemaVersion int64
	Tables        int
	Rows          int64
	BlobsTotal    int
	BlobsWritten  int
	// BlobsRepaired counts blobs already in blobsDir whose size did not match the database (a write cut
	// short before this version synced files), extracted again.
	BlobsRepaired int
}

type RestoreResult struct {
	SchemaVersion int64
	Tables        int
	Rows          int64
	BlobsRestored int
}

// Backup exports a point-in-time snapshot into snapshotDir and writes any document original not
// already present in blobsDir under its lowercase SHA-256 hex digest.
func Backup(ctx context.Context, pool *pgxpool.Pool, snapshotDir, blobsDir string) (BackupResult, error) {
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return BackupResult{}, fmt.Errorf("create snapshot dir: %w", err)
	}
	if err := os.MkdirAll(blobsDir, 0o700); err != nil {
		return BackupResult{}, fmt.Errorf("create blobs dir: %w", err)
	}
	snapRoot, err := os.OpenRoot(snapshotDir)
	if err != nil {
		return BackupResult{}, fmt.Errorf("open snapshot dir: %w", err)
	}
	defer func() { _ = snapRoot.Close() }()

	blobRoot, err := os.OpenRoot(blobsDir)
	if err != nil {
		return BackupResult{}, fmt.Errorf("open blobs dir: %w", err)
	}
	defer func() { _ = blobRoot.Close() }()

	if _, err := snapRoot.Stat(manifestFile); err == nil {
		return BackupResult{}, ErrSnapshotExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return BackupResult{}, fmt.Errorf("stat %s: %w", manifestFile, err)
	}
	if err := snapRoot.MkdirAll(tablesDir, 0o700); err != nil {
		return BackupResult{}, fmt.Errorf("create %s dir: %w", tablesDir, err)
	}

	poolQ := dbq.New(pool)
	hasSchema, err := poolQ.HasSchema(ctx)
	if err != nil {
		return BackupResult{}, fmt.Errorf("check schema: %w", err)
	}
	if !hasSchema {
		return BackupResult{}, errors.New("source database has no Finchie schema")
	}
	// A half-restored database is empty; backing it up would rotate good snapshots out of retention.
	pending, err := poolQ.HasRestorePending(ctx)
	if err != nil {
		return BackupResult{}, fmt.Errorf("check restore marker: %w", err)
	}
	if pending {
		return BackupResult{}, ErrRestorePending
	}
	schemaVersion, err := db.SchemaVersion(ctx, pool)
	if err != nil {
		return BackupResult{}, err
	}

	// One REPEATABLE READ transaction keeps every exported table and extracted blob on the exact
	// same point-in-time snapshot.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return BackupResult{}, fmt.Errorf("begin backup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := dbq.New(tx)
	tables, err := q.ListBackupTables(ctx)
	if err != nil {
		return BackupResult{}, fmt.Errorf("list tables: %w", err)
	}

	manifest := Manifest{
		Format:        FormatVersion,
		SchemaVersion: schemaVersion,
		CreatedAt:     time.Now().UTC(),
		Tables:        make([]TableManifest, 0, len(tables)),
	}
	result := BackupResult{
		SchemaVersion: schemaVersion,
		Tables:        len(tables),
	}

	for _, table := range tables {
		cols, err := q.ListTableColumns(ctx, table)
		if err != nil {
			return BackupResult{}, fmt.Errorf("list columns of %s: %w", table, err)
		}
		rows, err := copyTableOut(ctx, tx, snapRoot, table, cols)
		if err != nil {
			return BackupResult{}, err
		}
		manifest.Tables = append(manifest.Tables, TableManifest{
			Name:    table,
			Columns: cols,
			Rows:    rows,
		})
		result.Rows += rows
	}
	if err := syncDir(snapRoot, tablesDir); err != nil {
		return BackupResult{}, err
	}

	docs, err := q.ListDocumentBlobs(ctx)
	if err != nil {
		return BackupResult{}, fmt.Errorf("list document hashes: %w", err)
	}
	blobHexes := make([]string, 0, len(docs))
	seen := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		rawHash := doc.Sha256
		if len(rawHash) != sha256.Size {
			return BackupResult{}, fmt.Errorf("invalid document sha256 length %d", len(rawHash))
		}
		shaHex := hex.EncodeToString(rawHash)
		if _, dup := seen[shaHex]; dup {
			continue
		}
		seen[shaHex] = struct{}{}
		blobHexes = append(blobHexes, shaHex)

		// A blob already present is trusted when its size matches: it was written by rename after a
		// sync, so only a file cut short (a crash before syncing existed) differs, and that is written
		// again rather than kept for every later backup to skip. Hashes are checked by Restore.
		repair := false
		if info, err := blobRoot.Stat(shaHex); err == nil {
			if info.Size() == doc.Size {
				continue
			}
			repair = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return BackupResult{}, fmt.Errorf("stat blob %s: %w", shaHex, err)
		}

		content, err := q.GetDocumentContentByHash(ctx, rawHash)
		if err != nil {
			return BackupResult{}, fmt.Errorf("read document %s: %w", shaHex, err)
		}
		sum := sha256.Sum256(content)
		if !bytes.Equal(sum[:], rawHash) {
			return BackupResult{}, fmt.Errorf("%w: database document %s hashes to %x", ErrBlobCorrupt, shaHex, sum)
		}
		if err := writeFileAtomic(blobRoot, shaHex, content); err != nil {
			return BackupResult{}, fmt.Errorf("write blob %s: %w", shaHex, err)
		}
		if repair {
			result.BlobsRepaired++
		} else {
			result.BlobsWritten++
		}
	}
	slices.Sort(blobHexes)
	manifest.Blobs = blobHexes
	result.BlobsTotal = len(blobHexes)

	if err := tx.Commit(ctx); err != nil {
		return BackupResult{}, fmt.Errorf("commit read-only transaction: %w", err)
	}

	var listBuf strings.Builder
	for _, h := range blobHexes {
		listBuf.WriteString(h)
		listBuf.WriteByte('\n')
	}
	if err := writeFileAtomic(snapRoot, blobsListFile, []byte(listBuf.String())); err != nil {
		return BackupResult{}, fmt.Errorf("write %s: %w", blobsListFile, err)
	}

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BackupResult{}, fmt.Errorf("encode %s: %w", manifestFile, err)
	}
	manifestJSON = append(manifestJSON, '\n')
	// manifest.json is written last so its presence marks a complete snapshot.
	if err := writeFileAtomic(snapRoot, manifestFile, manifestJSON); err != nil {
		return BackupResult{}, fmt.Errorf("write %s: %w", manifestFile, err)
	}
	return result, nil
}

// Restore migrates an empty database to the snapshot's schema version, loads every table and
// document original in one transaction, verifies every document hash, and re-validates all foreign keys.
func Restore(ctx context.Context, pool *pgxpool.Pool, snapshotDir, blobsDir string) (RestoreResult, error) {
	snapRoot, err := os.OpenRoot(snapshotDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("open snapshot dir: %w", err)
	}
	defer func() { _ = snapRoot.Close() }()

	blobRoot, err := os.OpenRoot(blobsDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("open blobs dir: %w", err)
	}
	defer func() { _ = blobRoot.Close() }()

	manifestBytes, err := snapRoot.ReadFile(manifestFile)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read %s: %w", manifestFile, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return RestoreResult{}, fmt.Errorf("parse %s: %w", manifestFile, err)
	}
	if manifest.Format != FormatVersion {
		return RestoreResult{}, fmt.Errorf("%w: %d", ErrUnsupportedFormat, manifest.Format)
	}
	if manifest.SchemaVersion <= 0 {
		return RestoreResult{}, fmt.Errorf("invalid schema_version %d in %s", manifest.SchemaVersion, manifestFile)
	}

	// Verify every snapshot file and blob before touching the target database.
	for _, tm := range manifest.Tables {
		if tm.Name == "" || len(tm.Columns) == 0 {
			return RestoreResult{}, fmt.Errorf("invalid table entry %q in %s", tm.Name, manifestFile)
		}
		if _, err := snapRoot.Stat(tableCopyPath(tm.Name)); err != nil {
			return RestoreResult{}, fmt.Errorf("missing table dump %s: %w", tm.Name, err)
		}
	}
	blobHashes := make([][]byte, 0, len(manifest.Blobs))
	for _, shaHex := range manifest.Blobs {
		rawHash, err := parseSHA256Hex(shaHex)
		if err != nil {
			return RestoreResult{}, err
		}
		if err := verifyBlob(blobRoot, shaHex, rawHash); err != nil {
			return RestoreResult{}, err
		}
		blobHashes = append(blobHashes, rawHash)
	}

	// Migrations run outside the restore transaction (goose commits each one), so a restore that fails
	// later leaves a migrated schema behind. The marker created before migrating lets a retry tell that
	// schema from a database in use: the restore transaction truncates every table and drops the marker,
	// so a retry starts over from the same state.
	poolQ := dbq.New(pool)
	pending, err := poolQ.HasRestorePending(ctx)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("check restore marker: %w", err)
	}
	if !pending {
		hasSchema, err := poolQ.HasSchema(ctx)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("check target database: %w", err)
		}
		if hasSchema {
			return RestoreResult{}, ErrDatabaseNotEmpty
		}
		existingTables, err := poolQ.ListBackupTables(ctx)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("list target tables: %w", err)
		}
		if len(existingTables) > 0 {
			return RestoreResult{}, ErrDatabaseNotEmpty
		}
		if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (started_at TIMESTAMPTZ NOT NULL DEFAULT now())",
			pgx.Identifier{"public", restorePendingTable}.Sanitize())); err != nil {
			return RestoreResult{}, fmt.Errorf("create restore marker: %w", err)
		}
	}

	if err := db.MigrateTo(ctx, pool, manifest.SchemaVersion); err != nil {
		return RestoreResult{}, err
	}
	// A retry may meet a schema an earlier attempt, with a newer snapshot or a backend start, migrated
	// past this snapshot; migrations do not go back down.
	current, err := db.SchemaVersion(ctx, pool)
	if err != nil {
		return RestoreResult{}, err
	}
	if current != manifest.SchemaVersion {
		return RestoreResult{}, fmt.Errorf("%w: database is at schema version %d, snapshot is at %d; restore into a new database",
			ErrDatabaseNotEmpty, current, manifest.SchemaVersion)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("begin restore transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := dbq.New(tx)
	appTables, err := q.ListBackupTables(ctx)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("list migrated tables: %w", err)
	}
	appTableSet := make(map[string]struct{}, len(appTables))
	for _, t := range appTables {
		appTableSet[t] = struct{}{}
	}
	for _, tm := range manifest.Tables {
		if _, ok := appTableSet[tm.Name]; !ok {
			return RestoreResult{}, fmt.Errorf("snapshot table %q does not exist at schema version %d", tm.Name, manifest.SchemaVersion)
		}
	}

	// Drop foreign keys before COPY (transactions.counterpart_id is mutually self-referencing) and add
	// them back before commit so Postgres validates every reference. Using ALTER TABLE keeps restore
	// working under the regular table-owner role without superuser privileges.
	fks, err := q.ListForeignKeys(ctx, appTables)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("list foreign keys: %w", err)
	}
	for _, fk := range fks {
		dropSQL := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
			pgx.Identifier{"public", fk.TableName}.Sanitize(),
			pgx.Identifier{fk.ConstraintName}.Sanitize())
		if _, err := tx.Exec(ctx, dropSQL); err != nil {
			return RestoreResult{}, fmt.Errorf("drop foreign key %s.%s: %w", fk.TableName, fk.ConstraintName, err)
		}
	}

	// Clear rows seeded by migrations (NT$ unit, card point prices) so snapshot rows restore with their
	// original ids.
	if len(appTables) > 0 {
		quoted := make([]string, len(appTables))
		for i, t := range appTables {
			quoted[i] = pgx.Identifier{"public", t}.Sanitize()
		}
		truncateSQL := "TRUNCATE " + strings.Join(quoted, ", ") + " RESTART IDENTITY CASCADE"
		if _, err := tx.Exec(ctx, truncateSQL); err != nil {
			return RestoreResult{}, fmt.Errorf("truncate tables: %w", err)
		}
	}

	result := RestoreResult{
		SchemaVersion: manifest.SchemaVersion,
		Tables:        len(manifest.Tables),
		BlobsRestored: len(manifest.Blobs),
	}
	for _, tm := range manifest.Tables {
		rows, err := copyTableIn(ctx, tx, snapRoot, tm)
		if err != nil {
			return RestoreResult{}, err
		}
		if rows != tm.Rows {
			return RestoreResult{}, fmt.Errorf("table %s: restored %d rows, manifest expected %d", tm.Name, rows, tm.Rows)
		}
		result.Rows += rows
	}

	for i, shaHex := range manifest.Blobs {
		content, err := blobRoot.ReadFile(shaHex)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("read blob %s: %w", shaHex, err)
		}
		updated, err := q.RestoreDocumentContent(ctx, dbq.RestoreDocumentContentParams{
			Sha256:  blobHashes[i],
			Content: content,
		})
		if err != nil {
			return RestoreResult{}, fmt.Errorf("restore document %s: %w", shaHex, err)
		}
		if updated != 1 {
			return RestoreResult{}, fmt.Errorf("restore document %s: updated %d rows, want 1", shaHex, updated)
		}
	}
	mismatched, err := q.CountMismatchedDocumentHashes(ctx)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("verify document hashes: %w", err)
	}
	if mismatched != 0 {
		return RestoreResult{}, fmt.Errorf("%w: %d documents do not match their sha256", ErrBlobCorrupt, mismatched)
	}

	seqs, err := q.ListOwnedSequences(ctx, appTables)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("list owned sequences: %w", err)
	}
	for _, seq := range seqs {
		tableIdent := pgx.Identifier{"public", seq.TableName}.Sanitize()
		colIdent := pgx.Identifier{seq.ColumnName}.Sanitize()
		seqIdent := pgx.Identifier{"public", seq.SequenceName}.Sanitize()
		setvalSQL := fmt.Sprintf(
			"SELECT setval($1::regclass, COALESCE((SELECT max(%s) FROM %s), 1), EXISTS (SELECT 1 FROM %s))",
			colIdent, tableIdent, tableIdent)
		if _, err := tx.Exec(ctx, setvalSQL, seqIdent); err != nil {
			return RestoreResult{}, fmt.Errorf("sync sequence %s: %w", seq.SequenceName, err)
		}
	}

	for _, fk := range fks {
		addSQL := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s",
			pgx.Identifier{"public", fk.TableName}.Sanitize(),
			pgx.Identifier{fk.ConstraintName}.Sanitize(),
			fk.Definition)
		if _, err := tx.Exec(ctx, addSQL); err != nil {
			return RestoreResult{}, fmt.Errorf("restore foreign key %s.%s: %w", fk.TableName, fk.ConstraintName, err)
		}
	}

	if _, err := tx.Exec(ctx, "DROP TABLE "+pgx.Identifier{"public", restorePendingTable}.Sanitize()); err != nil {
		return RestoreResult{}, fmt.Errorf("drop restore marker: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("commit restore transaction: %w", err)
	}
	return result, nil
}

func copyTableOut(ctx context.Context, tx pgx.Tx, snapRoot *os.Root, table string, cols []string) (int64, error) {
	copySQL := buildCopyOutSQL(table, cols)
	f, err := snapRoot.OpenFile(tableCopyPath(table), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create dump for %s: %w", table, err)
	}
	gz := gzip.NewWriter(f)
	tag, copyErr := tx.Conn().PgConn().CopyTo(ctx, gz, copySQL)
	gzErr := gz.Close()
	var syncErr error
	if copyErr == nil && gzErr == nil {
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("copy out %s: %w", table, copyErr)
	}
	if gzErr != nil {
		return 0, fmt.Errorf("compress %s: %w", table, gzErr)
	}
	if syncErr != nil {
		return 0, fmt.Errorf("sync dump for %s: %w", table, syncErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close dump for %s: %w", table, closeErr)
	}
	return tag.RowsAffected(), nil
}

func copyTableIn(ctx context.Context, tx pgx.Tx, snapRoot *os.Root, tm TableManifest) (int64, error) {
	f, err := snapRoot.Open(tableCopyPath(tm.Name))
	if err != nil {
		return 0, fmt.Errorf("open dump for %s: %w", tm.Name, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("decompress %s: %w", tm.Name, err)
	}
	defer func() { _ = gz.Close() }()

	copySQL := fmt.Sprintf("COPY %s (%s) FROM STDIN",
		pgx.Identifier{"public", tm.Name}.Sanitize(),
		quoteColumns(tm.Columns))
	tag, err := tx.Conn().PgConn().CopyFrom(ctx, gz, copySQL)
	if err != nil {
		return 0, fmt.Errorf("copy in %s: %w", tm.Name, err)
	}
	return tag.RowsAffected(), nil
}

func buildCopyOutSQL(table string, cols []string) string {
	tableIdent := pgx.Identifier{"public", table}.Sanitize()
	if table == "documents" {
		exprs := make([]string, len(cols))
		for i, col := range cols {
			if col == "content" {
				// Empty bytea satisfies NOT NULL during restore before blobs are reattached.
				exprs[i] = "''::bytea AS \"content\""
			} else {
				exprs[i] = pgx.Identifier{col}.Sanitize()
			}
		}
		return fmt.Sprintf("COPY (SELECT %s FROM %s ORDER BY %s) TO STDOUT",
			strings.Join(exprs, ", "), tableIdent, pgx.Identifier{"id"}.Sanitize())
	}
	return fmt.Sprintf("COPY %s (%s) TO STDOUT", tableIdent, quoteColumns(cols))
}

func quoteColumns(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgx.Identifier{c}.Sanitize()
	}
	return strings.Join(quoted, ", ")
}

func tableCopyPath(table string) string {
	return path.Join(tablesDir, table+".copy.gz")
}

func parseSHA256Hex(shaHex string) ([]byte, error) {
	if len(shaHex) != sha256.Size*2 {
		return nil, fmt.Errorf("%w: invalid hash %q", ErrBlobCorrupt, shaHex)
	}
	raw, err := hex.DecodeString(shaHex)
	if err != nil || hex.EncodeToString(raw) != shaHex {
		return nil, fmt.Errorf("%w: invalid hash %q", ErrBlobCorrupt, shaHex)
	}
	return raw, nil
}

func verifyBlob(blobRoot *os.Root, shaHex string, want []byte) error {
	f, err := blobRoot.Open(shaHex)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrBlobMissing, shaHex)
		}
		return fmt.Errorf("open blob %s: %w", shaHex, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash blob %s: %w", shaHex, err)
	}
	if !bytes.Equal(h.Sum(nil), want) {
		return fmt.Errorf("%w: %s", ErrBlobCorrupt, shaHex)
	}
	return nil
}

// writeFileAtomic writes name through a synced temp file and a rename, then syncs the directory, so after
// a crash name is either absent or whole: never a short file a later backup would take for complete.
func writeFileAtomic(root *os.Root, name string, data []byte) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("random temp name: %w", err)
	}
	tmpName := fmt.Sprintf(".partial-%s-%x", name, suffix)
	f, err := root.OpenFile(tmpName, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		_ = root.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", writeErr)
	}
	if closeErr != nil {
		_ = root.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err := root.Rename(tmpName, name); err != nil {
		_ = root.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return syncDir(root, ".")
}

// syncDir makes the entries created or renamed in dir durable.
func syncDir(root *os.Root, dir string) error {
	d, err := root.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %s: %w", dir, err)
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return fmt.Errorf("sync dir %s: %w", dir, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close dir %s: %w", dir, closeErr)
	}
	return nil
}
