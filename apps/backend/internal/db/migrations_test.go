package db_test

import (
	"errors"
	"testing"

	"github.com/hsin19/finchie/apps/backend/internal/db"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

// Migrations that rewrite rows are tested on rows an older schema held. Synthetic figures (ADR 0006).

func TestOutcomesReadOutOfErrorsBecomeTheirOwnColumns(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := t.Context()
	if err := db.MigrateTo(ctx, pool, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status, error) VALUES
			('1', 'a.eml', 'message/rfc822', '', 'parsed', NULL),
			('2', 'b.eml', 'message/rfc822', '', 'failed', 'Unimplemented: no parser claims it'),
			('3', 'c.eml', 'message/rfc822', '', 'failed', 'FailedPrecondition: set PARSER_PDF_PASSWORDS__X'),
			('4', 'd.eml', 'message/rfc822', '', 'failed', 'Unavailable: connection refused'),
			('5', 'e.eml', 'message/rfc822', '', 'duplicate', 'same statement as document 1'),
			('6', 'f.eml', 'message/rfc822', '', 'failed', 'Internal: a line before its headings')`); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateTo(ctx, pool, 13); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `
		SELECT status || ' ' || coalesce(failure, '-') || ' ' || coalesce(duplicate_of::text, '-') || ' ' || coalesce(error, '-')
		FROM documents ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	want := []string{
		"parsed - - -",
		"skipped unsupported - no parser claims it",
		"failed locked - set PARSER_PDF_PASSWORDS__X",
		"failed unavailable - connection refused",
		"duplicate - 1 -",
		"failed layout - a line before its headings",
	}
	if len(got) != len(want) {
		t.Fatalf("documents = %q\nwant %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("documents = %q\nwant %q", got, want)
		}
	}
}

func TestStatementsAreKeyedByTheAccountsTheyReport(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := t.Context()
	if err := db.MigrateTo(ctx, pool, 13); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status) VALUES
			('1', 'bill.eml', 'message/rfc822', '', 'parsed'),
			('2', 'skis.eml', 'message/rfc822', '', 'parsed');
		INSERT INTO accounts (institution, kind, number) VALUES
			('esun', 'credit_card', ''), ('skis', 'securities', '12**345');
		INSERT INTO units (kind, symbol) VALUES ('security', '範例電子');
		INSERT INTO statements (document_id, institution, kind, closing_date, due_date, currency, total_due,
		                        computed_new_charges, stated_new_charges, stated_label, balanced)
		VALUES (1, 'esun', 'credit_card', '2026-10-21', '2026-11-08', 'TWD', 100, 100, 100, '本期新增', true);
		INSERT INTO statements (document_id, institution, kind, period_start, closing_date, balanced)
		VALUES (2, 'skis', 'securities', '2026-10-01', '2026-11-03', true);
		INSERT INTO balances (account_id, unit_id, as_of, amount, statement_id)
		SELECT a.id, u.id, '2026-11-03', 1000, s.id
		FROM accounts a, units u, statements s
		WHERE a.kind = 'securities' AND u.symbol = '範例電子' AND s.kind = 'securities'`); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateTo(ctx, pool, 14); err != nil {
		t.Fatal(err)
	}

	var bill, securities string
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT account_numbers FROM statements WHERE kind = 'credit_card'),
		       (SELECT account_numbers FROM statements WHERE kind = 'securities')`).Scan(&bill, &securities); err != nil {
		t.Fatal(err)
	}
	if bill != "" || securities != "12**345" {
		t.Fatalf("keys = %q, %q", bill, securities)
	}
	// Another account's statement of the same month is a statement of its own.
	if _, err := pool.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status) VALUES ('3', 'skis-2.eml', 'message/rfc822', '', 'parsed');
		INSERT INTO statements (document_id, institution, kind, account_numbers, period_start, closing_date, balanced)
		VALUES (3, 'skis', 'securities', '98**765', '2026-10-01', '2026-11-03', true)`); err != nil {
		t.Fatalf("a second account's statement: %v", err)
	}
}

func TestABackendRefusesADatabaseANewerOneMigrated(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := t.Context()
	if err := db.MigrateTo(ctx, pool, 1); err != nil {
		t.Fatal(err)
	}
	// As a later backend would have left it: a version this one has no migration for.
	if _, err := pool.Exec(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool); !errors.Is(err, db.ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
}

func TestABillsReconciliationBecomesItsFirstCheckAndWhatItOwesItsCardAccountsBalance(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := t.Context()
	if err := db.MigrateTo(ctx, pool, 19); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO documents (sha256, filename, content_type, content, status) VALUES ('1', 'bill.eml', 'message/rfc822', '', 'needs_review');
		INSERT INTO accounts (institution, kind, number) VALUES ('esun', 'credit_card', '');
		INSERT INTO statements (document_id, institution, kind, account_numbers, closing_date, due_date, currency, total_due,
		                        computed_new_charges, stated_new_charges, stated_label, balanced)
		VALUES (1, 'esun', 'credit_card', '', '2026-10-21', '2026-11-08', 'TWD', 3000, 2900, 3000, '本期新增款項', false)`); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateTo(ctx, pool, 20); err != nil {
		t.Fatal(err)
	}
	var check string
	if err := pool.QueryRow(ctx, `SELECT name || ' ' || unit || ' ' || computed || ' ' || stated || ' ' || balanced FROM statement_checks`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	if check != "本期新增款項 TWD 2900 3000 false" {
		t.Fatalf("check = %q", check)
	}
	var balance string
	if err := pool.QueryRow(ctx, `SELECT u.symbol || ' ' || b.as_of || ' ' || b.amount FROM balances b JOIN units u ON u.id = b.unit_id`).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != "TWD 2026-10-21 3000" {
		t.Fatalf("card balance = %q", balance)
	}
}
