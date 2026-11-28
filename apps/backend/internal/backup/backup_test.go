package backup_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsin19/finchie/apps/backend/internal/backup"
	"github.com/hsin19/finchie/apps/backend/internal/db"
	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func TestBackupAndRestoreRoundTrip(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}

	pdf1 := []byte("%PDF-1.7 synthetic bill bytes one\x00\xff\xfe")
	pdf2 := []byte("%PDF-1.7 synthetic bill bytes two (duplicate)")
	sum1 := sha256.Sum256(pdf1)
	sum2 := sha256.Sum256(pdf2)

	// Populate linked tables including self-referencing documents.duplicate_of and mutually
	// self-referencing transactions.counterpart_id. Synthetic figures (ADR 0006).
	if _, err := src.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status, parser, parser_version, warnings)
		VALUES
			($1, 'bill-1.eml', 'message/rfc822', $2, 'parsed', 'esun', '1.0.0', ARRAY['minor note']),
			($3, 'bill-2.eml', 'message/rfc822', $4, 'duplicate', 'esun', '1.0.0', '{}')`,
		sum1[:], pdf1, sum2[:], pdf2); err != nil {
		t.Fatalf("seed documents: %v", err)
	}
	if _, err := src.Exec(ctx, `
		UPDATE documents SET duplicate_of = (SELECT id FROM documents WHERE filename = 'bill-1.eml')
		WHERE filename = 'bill-2.eml';

		INSERT INTO accounts (institution, kind, number, name)
		VALUES
			('esun', 'credit_card', '', 'Main Card'),
			('taishin', 'deposit', '12**345', 'Main Deposit');

		INSERT INTO statements (document_id, institution, kind, account_numbers, period_start, closing_date, due_date, currency, total_due, balanced)
		VALUES
			((SELECT id FROM documents WHERE filename = 'bill-1.eml'), 'esun', 'credit_card', '', '2026-10-01', '2026-10-21', '2026-11-08', 'TWD', 1500, true);

		INSERT INTO statement_checks (statement_id, position, name, unit, computed, stated, balanced)
		VALUES
			((SELECT id FROM statements LIMIT 1), 0, 'New charges', 'TWD', 1500, 1500, true);

		INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
		VALUES
			((SELECT id FROM accounts WHERE kind = 'credit_card'), (SELECT id FROM units WHERE symbol = 'TWD'), '2026-10-21', 1500, (SELECT id FROM statements LIMIT 1)),
			((SELECT id FROM accounts WHERE kind = 'deposit'), (SELECT id FROM units WHERE symbol = 'TWD'), '2026-10-21', 50000, NULL);

		INSERT INTO transactions (statement_id, account_id, unit_id, line_no, transaction_date, kind, description, amount, memo, external_id)
		VALUES
			((SELECT id FROM statements LIMIT 1), (SELECT id FROM accounts WHERE kind = 'credit_card'), (SELECT id FROM units WHERE symbol = 'TWD'), 0, '2026-10-10', 'purchase', 'Coffee shop', 1500, '', NULL),
			((SELECT id FROM statements LIMIT 1), (SELECT id FROM accounts WHERE kind = 'credit_card'), (SELECT id FROM units WHERE symbol = 'TWD'), 1, '2026-10-11', 'foreign_transaction_fee', 'Foreign fee', 20, '', NULL),
			(NULL, (SELECT id FROM accounts WHERE kind = 'deposit'), (SELECT id FROM units WHERE symbol = 'TWD'), 0, '2026-10-25', 'payment', 'Pay card', -1500, 'autopay', 'ext-1');

		UPDATE transactions SET parent_id = (SELECT id FROM transactions WHERE description = 'Coffee shop')
		WHERE description = 'Foreign fee';
		UPDATE transactions SET counterpart_id = (SELECT id FROM transactions WHERE description = 'Pay card')
		WHERE description = 'Coffee shop';
		UPDATE transactions SET counterpart_id = (SELECT id FROM transactions WHERE description = 'Coffee shop')
		WHERE description = 'Pay card';
		UPDATE statements SET paid_by = (SELECT id FROM transactions WHERE description = 'Pay card');

		INSERT INTO payees (counterparty, name) VALUES ('RAW SHOP', 'Nice Shop');
		INSERT INTO push_keys (public_key, private_key) VALUES ('pub', 'priv');
		INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES ('https://push.example/1', 'k1', 'a1');`); err != nil {
		t.Fatalf("seed source db: %v", err)
	}

	tmp := t.TempDir()
	snap1 := filepath.Join(tmp, "snap-1")
	snap2 := filepath.Join(tmp, "snap-2")
	blobsDir := filepath.Join(tmp, "blobs")

	res1, err := backup.Backup(ctx, src, snap1, blobsDir)
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}
	if res1.BlobsTotal != 2 || res1.BlobsWritten != 2 {
		t.Fatalf("first backup blobs = total %d written %d, want 2/2", res1.BlobsTotal, res1.BlobsWritten)
	}

	// Verify documents.copy.gz does not carry raw document bytes.
	docDump := readGzipFile(t, snap1, "tables/documents.copy.gz")
	if bytes.Contains(docDump, []byte("synthetic bill bytes")) {
		t.Fatal("documents.copy.gz unexpectedly contains document content")
	}

	// Verify blobs.sha256 lists both hashes sorted.
	listBytes := readTestFile(t, snap1, "blobs.sha256")
	lines := strings.Split(strings.TrimSpace(string(listBytes)), "\n")
	if len(lines) != 2 {
		t.Fatalf("blobs.sha256 lines = %q, want 2", lines)
	}
	for _, want := range []struct {
		sum  [32]byte
		data []byte
	}{{sum1, pdf1}, {sum2, pdf2}} {
		got := readTestFile(t, blobsDir, hex.EncodeToString(want.sum[:]))
		if !bytes.Equal(got, want.data) {
			t.Fatalf("blob bytes mismatch")
		}
	}

	// A second snapshot sharing blobsDir reuses existing blobs without re-extracting.
	res2, err := backup.Backup(ctx, src, snap2, blobsDir)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if res2.BlobsTotal != 2 || res2.BlobsWritten != 0 {
		t.Fatalf("second backup blobs = total %d written %d, want 2/0", res2.BlobsTotal, res2.BlobsWritten)
	}

	// Restore into a brand new empty database.
	dst := testdb.Empty(t)
	restoreRes, err := backup.Restore(ctx, dst, snap1, blobsDir)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restoreRes.SchemaVersion != res1.SchemaVersion || restoreRes.Rows != res1.Rows || restoreRes.BlobsRestored != 2 {
		t.Fatalf("restore result = %+v, want schema %d rows %d blobs 2", restoreRes, res1.SchemaVersion, res1.Rows)
	}

	// Compare documents content and links.
	var gotPDF1, gotPDF2 []byte
	var dupOf int64
	if err := dst.QueryRow(ctx, `
		SELECT (SELECT content FROM documents WHERE filename = 'bill-1.eml'),
		       (SELECT content FROM documents WHERE filename = 'bill-2.eml'),
		       (SELECT duplicate_of FROM documents WHERE filename = 'bill-2.eml')`).Scan(&gotPDF1, &gotPDF2, &dupOf); err != nil {
		t.Fatalf("query restored documents: %v", err)
	}
	if !bytes.Equal(gotPDF1, pdf1) || !bytes.Equal(gotPDF2, pdf2) {
		t.Fatal("restored document content does not match original")
	}
	if dupOf == 0 {
		t.Fatal("duplicate_of was not preserved")
	}

	// Verify circular counterpart_id and parent_id links survived.
	var linked int
	if err := dst.QueryRow(ctx, `
		SELECT count(*) FROM transactions WHERE counterpart_id IS NOT NULL`).Scan(&linked); err != nil {
		t.Fatalf("query linked transactions: %v", err)
	}
	if linked != 2 {
		t.Fatalf("linked transactions = %d, want 2", linked)
	}

	// Verify identity sequences were synced so new inserts do not collide with restored ids.
	var newDocID int64
	pdf3 := []byte("brand new statement after restore")
	sum3 := sha256.Sum256(pdf3)
	if err := dst.QueryRow(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status)
		VALUES ($1, 'bill-3.eml', 'message/rfc822', $2, 'pending')
		RETURNING id`, sum3[:], pdf3).Scan(&newDocID); err != nil {
		t.Fatalf("insert after restore failed (sequence out of sync?): %v", err)
	}
	if newDocID <= 2 {
		t.Fatalf("new document id = %d, want > 2", newDocID)
	}

	// Normal startup Migrate (goose + River) succeeds on the restored database.
	if err := db.Migrate(ctx, dst); err != nil {
		t.Fatalf("post-restore Migrate: %v", err)
	}
}

func TestRestoreOlderSchemaMigratesForward(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.MigrateTo(ctx, src, 28); err != nil {
		t.Fatal(err)
	}
	body := []byte("older schema document")
	sum := sha256.Sum256(body)
	if _, err := src.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status)
		VALUES ($1, 'old.eml', 'message/rfc822', $2, 'pending')`, sum[:], body); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	snap := filepath.Join(tmp, "snap")
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, src, snap, blobs); err != nil {
		t.Fatalf("backup at v28: %v", err)
	}

	dst := testdb.Empty(t)
	res, err := backup.Restore(ctx, dst, snap, blobs)
	if err != nil {
		t.Fatalf("restore at v28: %v", err)
	}
	if res.SchemaVersion != 28 {
		t.Fatalf("restored schema version = %d, want 28", res.SchemaVersion)
	}
	current, err := db.SchemaVersion(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if current != 28 {
		t.Fatalf("database version after restore = %d, want 28", current)
	}
	if err := db.Migrate(ctx, dst); err != nil {
		t.Fatalf("migrate restored v28 forward: %v", err)
	}
}

func TestRestoreRefusesNewerSchema(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snap := filepath.Join(tmp, "snap")
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, src, snap, blobs); err != nil {
		t.Fatal(err)
	}

	raw := readTestFile(t, snap, "manifest.json")
	var m backup.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.SchemaVersion = 99999
	updated, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "manifest.json"), updated, 0o600); err != nil {
		t.Fatal(err)
	}

	dst := testdb.Empty(t)
	_, err = backup.Restore(ctx, dst, snap, blobs)
	if !errors.Is(err, db.ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
}

func TestRestoreRefusesMissingOrCorruptBlobBeforeTouchingDatabase(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}
	body := []byte("real document content")
	sum := sha256.Sum256(body)
	if _, err := src.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status)
		VALUES ($1, 'doc.eml', 'message/rfc822', $2, 'pending')`, sum[:], body); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	snap := filepath.Join(tmp, "snap")
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, src, snap, blobs); err != nil {
		t.Fatal(err)
	}

	blobPath := filepath.Join(blobs, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(blobPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := testdb.Empty(t)
	if _, err := backup.Restore(ctx, dst, snap, blobs); !errors.Is(err, backup.ErrBlobCorrupt) {
		t.Fatalf("corrupt blob err = %v, want ErrBlobCorrupt", err)
	}

	if err := os.Remove(blobPath); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(ctx, dst, snap, blobs); !errors.Is(err, backup.ErrBlobMissing) {
		t.Fatalf("missing blob err = %v, want ErrBlobMissing", err)
	}

	// Target DB was never migrated or modified.
	var hasGoose bool
	if err := dst.QueryRow(ctx, "SELECT to_regclass('public.goose_db_version') IS NOT NULL").Scan(&hasGoose); err != nil {
		t.Fatal(err)
	}
	if hasGoose {
		t.Fatal("target database was touched despite invalid blobs")
	}
}

func TestRestoreRefusesNonEmptyDatabaseAndBackupRefusesExistingSnapshot(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snap := filepath.Join(tmp, "snap")
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, pool, snap, blobs); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Backup(ctx, pool, snap, blobs); !errors.Is(err, backup.ErrSnapshotExists) {
		t.Fatalf("duplicate backup err = %v, want ErrSnapshotExists", err)
	}
	if _, err := backup.Restore(ctx, pool, snap, blobs); !errors.Is(err, backup.ErrDatabaseNotEmpty) {
		t.Fatalf("non-empty restore err = %v, want ErrDatabaseNotEmpty", err)
	}
}

func TestBackupRewritesBlobCutShort(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}
	body := []byte("statement whose blob a crash cut short")
	sum := sha256.Sum256(body)
	if _, err := src.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status)
		VALUES ($1, 'cut.eml', 'message/rfc822', $2, 'pending')`, sum[:], body); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, src, filepath.Join(tmp, "snap-1"), blobs); err != nil {
		t.Fatal(err)
	}
	// What a crash between rename and the data reaching disk could leave without the syncs.
	blobPath := filepath.Join(blobs, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(blobPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := backup.Backup(ctx, src, filepath.Join(tmp, "snap-2"), blobs)
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if res.BlobsWritten != 0 || res.BlobsRepaired != 1 {
		t.Fatalf("second backup blobs = written %d repaired %d, want 0/1", res.BlobsWritten, res.BlobsRepaired)
	}
	if got := readTestFile(t, blobs, hex.EncodeToString(sum[:])); !bytes.Equal(got, body) {
		t.Fatalf("blob after repair = %q, want the original", got)
	}
}

func TestRestoreFailedMidwayCanBeRetriedOnTheSameDatabase(t *testing.T) {
	src := testdb.Empty(t)
	ctx := t.Context()
	if err := db.Migrate(ctx, src); err != nil {
		t.Fatal(err)
	}
	body := []byte("document restored on the second try")
	sum := sha256.Sum256(body)
	if _, err := src.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status)
		VALUES ($1, 'retry.eml', 'message/rfc822', $2, 'pending')`, sum[:], body); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snap := filepath.Join(tmp, "snap")
	blobs := filepath.Join(tmp, "blobs")
	if _, err := backup.Backup(ctx, src, snap, blobs); err != nil {
		t.Fatal(err)
	}

	// A row count that disagrees with the dump fails the restore after it has migrated the database.
	good := readTestFile(t, snap, "manifest.json")
	var m backup.Manifest
	if err := json.Unmarshal(good, &m); err != nil {
		t.Fatal(err)
	}
	for i := range m.Tables {
		if m.Tables[i].Name == "documents" {
			m.Tables[i].Rows++
		}
	}
	bad, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(snap, "manifest.json")
	if err := os.WriteFile(manifestPath, bad, 0o600); err != nil {
		t.Fatal(err)
	}

	dst := testdb.Empty(t)
	if _, err := backup.Restore(ctx, dst, snap, blobs); err == nil || errors.Is(err, backup.ErrDatabaseNotEmpty) {
		t.Fatalf("restore with a wrong row count err = %v, want a load failure", err)
	}
	pending, err := dbq.New(dst).HasRestorePending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("failed restore left no marker")
	}
	if _, err := backup.Backup(ctx, dst, filepath.Join(tmp, "snap-of-half"), blobs); !errors.Is(err, backup.ErrRestorePending) {
		t.Fatalf("backup of a half-restored database err = %v, want ErrRestorePending", err)
	}

	if err := os.WriteFile(manifestPath, good, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(ctx, dst, snap, blobs); err != nil {
		t.Fatalf("retry restore: %v", err)
	}
	if pending, err := dbq.New(dst).HasRestorePending(ctx); err != nil || pending {
		t.Fatalf("marker after a finished restore = %v (err %v), want gone", pending, err)
	}
	var got []byte
	if err := dst.QueryRow(ctx, "SELECT content FROM documents WHERE filename = 'retry.eml'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("restored content does not match")
	}
	// Finished, the database is in use again: a further restore is refused.
	if _, err := backup.Restore(ctx, dst, snap, blobs); !errors.Is(err, backup.ErrDatabaseNotEmpty) {
		t.Fatalf("restore over a restored database err = %v, want ErrDatabaseNotEmpty", err)
	}
}

func readTestFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readGzipFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(readTestFile(t, dir, name)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
