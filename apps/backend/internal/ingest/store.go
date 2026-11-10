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
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

// ErrInvalidResponse means the parser returned something that breaks the contract; retrying won't help.
var ErrInvalidResponse = errors.New("invalid parser response")

var kinds = map[parserv1.TransactionKind]domain.LineKind{
	parserv1.TransactionKind_TRANSACTION_KIND_PURCHASE:                domain.Purchase,
	parserv1.TransactionKind_TRANSACTION_KIND_FOREIGN_TRANSACTION_FEE: domain.ForeignTransactionFee,
	parserv1.TransactionKind_TRANSACTION_KIND_ANNUAL_FEE:              domain.AnnualFee,
	parserv1.TransactionKind_TRANSACTION_KIND_REFUND:                  domain.Refund,
	parserv1.TransactionKind_TRANSACTION_KIND_REBATE:                  domain.Rebate,
	parserv1.TransactionKind_TRANSACTION_KIND_PAYMENT:                 domain.Payment,
}

var pointsKinds = map[parserv1.PointsLineKind]domain.LineKind{
	parserv1.PointsLineKind_POINTS_LINE_KIND_EARN:   domain.Earn,
	parserv1.PointsLineKind_POINTS_LINE_KIND_ADJUST: domain.Adjust,
	parserv1.PointsLineKind_POINTS_LINE_KIND_REDEEM: domain.Redeem,
}

var depositKinds = map[parserv1.DepositLineKind]domain.LineKind{
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER:    domain.Other,
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_INTEREST: domain.Interest,
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_FEE:      domain.Fee,
}

// stored is a statement of any kind, converted and ready to store.
type stored struct {
	statement dbq.InsertStatementParams
	accounts  []storedAccount
	// A securities statement's trades' money, by line number (ADR 0017).
	trades dbq.InsertTradesParams
	// Accounts whose lines do not add up, said in words for the document's warnings.
	unbalanced []string
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
func Store(ctx context.Context, pool *pgxpool.Pool, documentID int64, instance string, response *parserv1.ParseDocumentResponse) (domain.DocumentStatus, error) {
	result, err := convertResponse(documentID, response)
	if err != nil {
		return "", err
	}
	finish := dbq.FinishDocumentParams{
		ID:             documentID,
		ParserInstance: optionalString(instance),
		Parser:         &response.Parser,
		ParserVersion:  &response.ParserVersion,
		Warnings:       append(append([]string{}, response.GetWarnings()...), result.unbalanced...),
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
			// Each section reports one currency, and always its closing balance.
			if err := trimOverlaps(ctx, queries, this, account.id, account.unitID(account.units, account.balances[0].unit)); err != nil {
				return err
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

func convertResponse(documentID int64, response *parserv1.ParseDocumentResponse) (stored, error) {
	result, err := convertStatement(documentID, response)
	if err != nil {
		return stored{}, err
	}
	// A statement is the accounts it reports over its period (ADR 0013).
	numbers := make([]string, 0, len(result.accounts))
	for _, a := range result.accounts {
		numbers = append(numbers, a.account.Number)
	}
	result.statement.AccountNumbers = domain.AccountNumbers(numbers...)
	return result, nil
}

func convertStatement(documentID int64, response *parserv1.ParseDocumentResponse) (stored, error) {
	bill, deposit, securities := response.GetStatement(), response.GetDepositStatement(), response.GetSecuritiesStatement()
	switch {
	case bill != nil && deposit == nil && securities == nil:
		statement, lines, err := convert(documentID, bill)
		if err != nil {
			return stored{}, err
		}
		// A bank's card lines belong to its credit_card account, which holds no balance: what the bill
		// says is owed stays on the bill (ADR 0013).
		card := storedAccount{account: dbq.UpsertAccountParams{Institution: statement.Institution, Kind: domain.CreditCard}}
		for _, line := range lines {
			card.lines = append(card.lines, storedLine{unit: *statement.Currency, InsertTransactionsParams: line})
		}
		accounts := []storedAccount{card}
		// Its points lines follow its card lines: a statement numbers its lines once.
		next := int32(0)
		if len(lines) > 0 {
			next = lines[len(lines)-1].LineNo + 1
		}
		points, err := convertPoints(bill, statement.ClosingDate, next)
		if err != nil {
			return stored{}, err
		}
		if points != nil {
			points.account = dbq.UpsertAccountParams{Institution: statement.Institution, Kind: domain.Points}
			accounts = append(accounts, *points)
		}
		return stored{statement: statement, accounts: accounts}, nil
	case deposit != nil && bill == nil && securities == nil:
		return convertDeposit(documentID, deposit)
	case securities != nil && bill == nil && deposit == nil:
		return convertSecurities(documentID, securities)
	default:
		return stored{}, fmt.Errorf("%w: exactly one of statement, deposit statement, and securities statement must be set", ErrInvalidResponse)
	}
}

func convert(documentID int64, s *parserv1.Statement) (dbq.InsertStatementParams, []dbq.InsertTransactionsParams, error) {
	if s == nil || s.GetReconciliation() == nil {
		return dbq.InsertStatementParams{}, nil, fmt.Errorf("%w: missing statement", ErrInvalidResponse)
	}
	var errs []error

	rec := s.GetReconciliation()
	statement := dbq.InsertStatementParams{
		DocumentID:  documentID,
		Institution: s.GetInstitution(),
		Kind:        domain.CreditCard,
		Currency:    optionalString(s.GetCurrency()),
		StatedLabel: optionalString(rec.GetStatedLabel()),
		Balanced:    rec.GetBalanced(),
	}
	if statement.Currency == nil {
		errs = append(errs, errors.New("currency is missing"))
	}
	statement.ClosingDate, errs = requiredDate(s.GetClosingDate(), "closing date", errs)
	statement.DueDate, errs = requiredDate(s.GetDueDate(), "due date", errs)
	statement.TotalDue, errs = requiredNumeric(s.GetTotalDue(), "total due", errs)
	statement.ComputedNewCharges, errs = requiredNumeric(rec.GetComputed(), "computed", errs)
	statement.StatedNewCharges, errs = requiredNumeric(rec.GetStated(), "stated", errs)

	transactions := make([]dbq.InsertTransactionsParams, 0, len(s.GetTransactions()))
	for i, t := range s.GetTransactions() {
		kind, ok := kinds[t.GetKind()]
		if !ok {
			errs = append(errs, fmt.Errorf("transaction %d: kind %s", i, t.GetKind()))
		}
		row := dbq.InsertTransactionsParams{
			LineNo:           int32(i),
			Description:      t.GetDescription(),
			Kind:             kind,
			OriginalCurrency: optionalString(t.GetOriginalCurrency()),
			Country:          optionalString(t.GetCountry()),
			CardLast4:        optionalString(t.GetCardLast4()),
		}
		row.TransactionDate, errs = requiredDate(t.GetTransactionDate(), fmt.Sprintf("transaction %d date", i), errs)
		posting, err := optionalDate(t.GetPostingDate())
		if err != nil {
			errs = append(errs, fmt.Errorf("transaction %d posting date: %w", i, err))
		}
		row.PostingDate = posting
		row.Amount, errs = requiredNumeric(t.GetAmount(), fmt.Sprintf("transaction %d amount", i), errs)
		original, err := numeric(t.GetOriginalAmount())
		if err != nil {
			errs = append(errs, fmt.Errorf("transaction %d original amount: %w", i, err))
		}
		row.OriginalAmount = original
		transactions = append(transactions, row)
	}

	if len(errs) > 0 {
		return dbq.InsertStatementParams{}, nil, fmt.Errorf("%w: %w", ErrInvalidResponse, errors.Join(errs...))
	}
	return statement, transactions, nil
}

// convertPoints takes a card bill's programs: each one's closing as the balance of its unit on the
// closing date, its period's movements as lines on that date, and what it says will lapse (ADR 0019).
// Nil when the bill reports none.
func convertPoints(s *parserv1.Statement, closing pgtype.Date, lineNo int32) (*storedAccount, error) {
	if len(s.GetPoints()) == 0 {
		return nil, nil
	}
	var errs []error
	var points storedAccount
	for i, p := range s.GetPoints() {
		if p.GetName() == "" {
			errs = append(errs, fmt.Errorf("points program %d: name is missing", i))
		}
		balance := storedBalance{unit: p.GetName(), InsertBalanceParams: dbq.InsertBalanceParams{AsOf: closing}}
		balance.Amount, errs = requiredNumeric(p.GetClosing(), fmt.Sprintf("points program %d closing", i), errs)
		points.balances = append(points.balances, balance)
		for j, l := range p.GetLines() {
			kind, ok := pointsKinds[l.GetKind()]
			if !ok {
				errs = append(errs, fmt.Errorf("points program %d line %d: kind %s", i, j, l.GetKind()))
			}
			line := storedLine{unit: p.GetName(), InsertTransactionsParams: dbq.InsertTransactionsParams{
				LineNo: lineNo, TransactionDate: closing, Description: l.GetLabel(), Kind: kind,
			}}
			lineNo++
			line.Amount, errs = requiredNumeric(l.GetAmount(), fmt.Sprintf("points program %d line %d amount", i, j), errs)
			points.lines = append(points.lines, line)
		}
		for j, e := range p.GetExpiries() {
			expiry := storedExpiry{unit: p.GetName()}
			expiry.Amount, errs = requiredNumeric(e.GetAmount(), fmt.Sprintf("points program %d expiry %d amount", i, j), errs)
			expiry.LapsesBy, errs = requiredDate(e.GetBy(), fmt.Sprintf("points program %d expiry %d date", i, j), errs)
			points.expiries = append(points.expiries, expiry)
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidResponse, errors.Join(errs...))
	}
	return &points, nil
}

// convertDeposit takes a deposit statement: its lines and closing balances by account, and an opening
// balance where printed, as the balance at the end of the day before its period (ADR 0013).
func convertDeposit(documentID int64, d *parserv1.DepositStatement) (stored, error) {
	var errs []error
	statement := dbq.InsertStatementParams{
		DocumentID:  documentID,
		Institution: d.GetInstitution(),
		Kind:        domain.Deposit,
		Balanced:    true,
	}
	if statement.Institution == "" {
		errs = append(errs, errors.New("institution is missing"))
	}
	statement.PeriodStart, errs = requiredDate(d.GetPeriodStart(), "period start", errs)
	statement.ClosingDate, errs = requiredDate(d.GetPeriodEnd(), "period end", errs)
	if len(d.GetAccounts()) == 0 {
		errs = append(errs, errors.New("no accounts"))
	}

	result := stored{}
	seen := map[[2]string]bool{}
	lineNo := int32(0)
	for i, a := range d.GetAccounts() {
		where := fmt.Sprintf("account %d", i)
		key := [2]string{a.GetNumber(), a.GetCurrency()}
		switch {
		case a.GetNumber() == "" || a.GetCurrency() == "":
			errs = append(errs, fmt.Errorf("%s: number and currency are required", where))
		case seen[key]:
			errs = append(errs, fmt.Errorf("%s: %s %s reported twice", where, key[0], key[1]))
		}
		seen[key] = true

		reported := storedAccount{account: dbq.UpsertAccountParams{
			Institution: statement.Institution, Kind: domain.Deposit, Number: a.GetNumber(), Label: optionalString(a.GetName()),
		}}
		var closing pgtype.Numeric
		closing, errs = requiredNumeric(a.GetClosingBalance(), where+" closing balance", errs)
		reported.balances = append(reported.balances, storedBalance{unit: a.GetCurrency(),
			InsertBalanceParams: dbq.InsertBalanceParams{AsOf: statement.ClosingDate, Amount: closing}})
		opening, err := numeric(a.GetOpeningBalance())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s opening balance: %w", where, err))
		}
		if opening.Valid && statement.PeriodStart.Valid {
			dayBefore := pgtype.Date{Time: statement.PeriodStart.Time.AddDate(0, 0, -1), Valid: true}
			reported.balances = append(reported.balances, storedBalance{unit: a.GetCurrency(),
				InsertBalanceParams: dbq.InsertBalanceParams{AsOf: dayBefore, Amount: opening}})
		}

		for j, l := range a.GetLines() {
			at := fmt.Sprintf("%s line %d", where, j)
			kind, ok := depositKinds[l.GetKind()]
			if !ok {
				errs = append(errs, fmt.Errorf("%s: kind %s", at, l.GetKind()))
			}
			row := storedLine{unit: a.GetCurrency(), InsertTransactionsParams: dbq.InsertTransactionsParams{
				LineNo:       lineNo,
				Description:  l.GetDescription(),
				Kind:         kind,
				Memo:         optionalString(l.GetMemo()),
				Counterparty: optionalString(l.GetCounterparty()),
			}}
			lineNo++
			row.TransactionDate, errs = requiredDate(l.GetDate(), at+" date", errs)
			row.Amount, errs = requiredNumeric(l.GetAmount(), at+" amount", errs)
			if row.Balance, err = numeric(l.GetBalance()); err != nil {
				errs = append(errs, fmt.Errorf("%s balance: %w", at, err))
			}
			reported.lines = append(reported.lines, row)
		}

		if rec := a.GetReconciliation(); rec != nil && !rec.GetBalanced() {
			statement.Balanced = false
			result.unbalanced = append(result.unbalanced, fmt.Sprintf("%s %s %s: the lines come to %s, %s is %s",
				a.GetName(), a.GetNumber(), a.GetCurrency(), rec.GetComputed().GetValue(), rec.GetStatedLabel(), rec.GetStated().GetValue()))
		}
		result.accounts = append(result.accounts, reported)
	}

	if len(errs) > 0 {
		return stored{}, fmt.Errorf("%w: %w", ErrInvalidResponse, errors.Join(errs...))
	}
	result.statement = statement
	return result, nil
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
