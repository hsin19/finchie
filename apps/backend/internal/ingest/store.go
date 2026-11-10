package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

// ErrInvalidResponse means the parser returned something that breaks the contract; retrying won't help.
var ErrInvalidResponse = errors.New("invalid parser response")

// stored is a statement of any kind, converted and ready to store.
type stored struct {
	statement dbq.InsertStatementParams
	accounts  []storedAccount
	// A securities statement's trades' money, by line number (ADR 0017).
	trades dbq.InsertTradesParams
	// Every figure it prints that its lines are checked against (ADR 0021).
	checks dbq.InsertStatementChecksParams
}

// storedAccount is what the statement reports of one account; ids are filled in when stored.
type storedAccount struct {
	account  dbq.UpsertAccountParams
	lines    []storedLine
	balances []storedBalance
	// What will lapse of a points account's units (ADR 0019).
	expiries []storedExpiry
}

// Store replaces everything previously derived from the document with the parser's result, in one
// transaction, and returns the document's new status: the statement unless another document holds it,
// what the statement's kind derives from it, and every link worked out again.
func Store(ctx context.Context, pool *pgxpool.Pool, documentID int64, instance string, response *parserv2.ParseDocumentResponse) (domain.DocumentStatus, error) {
	result, err := convertResponse(documentID, response)
	if err != nil {
		return "", err
	}
	finish := dbq.FinishDocumentParams{
		ID:             documentID,
		ParserInstance: optionalString(instance),
		Parser:         &response.Parser,
		ParserVersion:  &response.ParserVersion,
		Warnings:       append([]string{}, response.GetWarnings()...),
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		holder, err := replace(ctx, queries, documentID, &result.statement)
		if err != nil {
			return err
		}
		if holder != nil {
			// Another document already holds this statement (an e-mail and the same one forwarded).
			finish.Status, finish.DuplicateOf = domain.Duplicate, holder
		} else {
			if err := insert(ctx, queries, result); err != nil {
				return err
			}
			finish.Status = domain.Outcome(result.statement.Balanced)
		}
		// Its lines, or the ones it had before, may be parents or children of lines on other statements.
		if _, err := ledger.Relink(ctx, queries); err != nil {
			return err
		}
		return queries.FinishDocument(ctx, finish)
	})
	if err != nil {
		return "", fmt.Errorf("store parse result: %w", err)
	}
	return finish.Status, nil
}

// replace deletes what the document derived before and, when another document already holds the same
// statement, returns that document. A re-parse keeps the statement's id: its page and the links to it stay
// where they were.
func replace(ctx context.Context, queries *dbq.Queries, documentID int64, statement *dbq.InsertStatementParams) (*int64, error) {
	// Before deleting anything: relinking (in Store, and in a statement stored at the same time) updates
	// every linked line, so taking the lock only then would let each side hold rows the other waits for.
	if err := queries.LockLinks(ctx); err != nil {
		return nil, fmt.Errorf("lock links: %w", err)
	}
	previous, err := queries.DeleteStatementsForDocument(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("delete statements: %w", err)
	}
	if len(previous) > 0 {
		statement.ID = &previous[0]
	}
	existing, err := queries.FindStatementByKey(ctx, dbq.FindStatementByKeyParams{
		Institution:    statement.Institution,
		Kind:           statement.Kind,
		AccountNumbers: statement.AccountNumbers,
		PeriodStart:    statement.PeriodStart,
		ClosingDate:    statement.ClosingDate,
	})
	switch {
	case err == nil:
		return &existing.DocumentID, nil
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	default:
		return nil, fmt.Errorf("find statement: %w", err)
	}
}

// insert stores the statement and what it reports of each account, then what its kind derives from them.
func insert(ctx context.Context, queries *dbq.Queries, result stored) error {
	statementID, err := queries.InsertStatement(ctx, result.statement)
	if err != nil {
		return fmt.Errorf("insert statement: %w", err)
	}
	if len(result.checks.Names) > 0 {
		checks := result.checks
		checks.StatementID = statementID
		if err := queries.InsertStatementChecks(ctx, checks); err != nil {
			return fmt.Errorf("insert checks: %w", err)
		}
	}
	var lines []dbq.InsertTransactionsParams
	found := make([]foundAccount, 0, len(result.accounts))
	for _, reported := range result.accounts {
		// Found, or created the first time a statement reports it (ADR 0013); likewise its units (ADR 0020).
		accountID, err := queries.UpsertAccount(ctx, reported.account)
		if err != nil {
			return fmt.Errorf("find account: %w", err)
		}
		units, err := reported.unitIDs(ctx, queries)
		if err != nil {
			return err
		}
		found = append(found, foundAccount{storedAccount: reported, id: accountID, units: units})
		for _, line := range reported.lines {
			line.StatementID, line.AccountID, line.UnitID = statementID, accountID, reported.unitID(units, line.unit)
			lines = append(lines, line.InsertTransactionsParams)
		}
		for _, balance := range reported.balances {
			balance.StatementID, balance.AccountID, balance.UnitID = statementID, accountID, reported.unitID(units, balance.unit)
			if err := queries.InsertBalance(ctx, balance.InsertBalanceParams); err != nil {
				return fmt.Errorf("record balance: %w", err)
			}
		}
		for _, expiry := range reported.expiries {
			expiry.StatementID, expiry.AccountID, expiry.UnitID = statementID, accountID, reported.unitID(units, expiry.unit)
			if err := queries.InsertExpiry(ctx, expiry.InsertExpiryParams); err != nil {
				return fmt.Errorf("record expiry: %w", err)
			}
		}
	}
	if _, err := queries.InsertTransactions(ctx, lines); err != nil {
		return fmt.Errorf("insert lines: %w", err)
	}

	switch result.statement.Kind {
	case domain.Deposit:
		this := span{id: statementID, documentID: result.statement.DocumentID, start: result.statement.PeriodStart.Time, end: result.statement.ClosingDate.Time}
		for _, account := range found {
			for _, unit := range account.balanceUnits() {
				if err := trimOverlaps(ctx, queries, this, account.id, account.unitID(account.units, unit)); err != nil {
					return err
				}
			}
		}
	case domain.Securities:
		accountIDs := make([]int64, 0, len(found))
		for _, account := range found {
			accountIDs = append(accountIDs, account.id)
		}
		if err := storeHoldings(ctx, queries, statementID, result.trades, accountIDs); err != nil {
			return err
		}
	}
	return nil
}

// foundAccount is what a statement reports of an account, with the account and its units found.
type foundAccount struct {
	storedAccount
	id    int64
	units map[string]int64
}

func requiredDate(d *date.Date, field string, errs []error) (pgtype.Date, []error) {
	value, err := optionalDate(d)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", field, err))
	} else if !value.Valid {
		errs = append(errs, fmt.Errorf("%s is missing", field))
	}
	return value, errs
}

// optionalDate takes an unset or all-zero date as none. Anything else must be a whole calendar date
// within google.type.Date's years: time.Date would quietly roll 2026-02-30 over into March, and a
// missing month or day into the one before.
func optionalDate(d *date.Date) (pgtype.Date, error) {
	year, month, day := int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay())
	if year == 0 && month == 0 && day == 0 {
		return pgtype.Date{}, nil
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if year < 1 || year > 9999 || t.Month() != month || t.Day() != day {
		return pgtype.Date{}, fmt.Errorf("%04d-%02d-%02d is not a date", year, month, day)
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func requiredNumeric(d *decimal.Decimal, field string, errs []error) (pgtype.Numeric, []error) {
	value, err := numeric(d)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", field, err))
	} else if !value.Valid {
		errs = append(errs, fmt.Errorf("%s is missing", field))
	}
	return value, errs
}

func numeric(d *decimal.Decimal) (pgtype.Numeric, error) {
	var value pgtype.Numeric
	if d == nil || d.GetValue() == "" {
		return value, nil
	}
	if err := value.Scan(d.GetValue()); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("decimal %q: %w", d.GetValue(), err)
	}
	// A NUMERIC column takes NaN and infinities, but no amount is one, and the API's totals add
	// amounts assuming they are finite.
	if value.NaN || value.InfinityModifier != pgtype.Finite {
		return pgtype.Numeric{}, fmt.Errorf("decimal %q is not a finite number", d.GetValue())
	}
	return value, nil
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
