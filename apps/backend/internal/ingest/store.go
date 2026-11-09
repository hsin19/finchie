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
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
)

// Document statuses, as stored in documents.status.
const (
	StatusPending     = "pending"
	StatusParsed      = "parsed"
	StatusNeedsReview = "needs_review"
	StatusDuplicate   = "duplicate"
	StatusFailed      = "failed"
)

// Kinds of statement, as stored in statements.kind, and of account, in accounts.kind (ADR 0013): a
// card bill reports a credit card account, a deposit statement deposit accounts, a securities
// statement a brokerage account (ADR 0017), and a card bill the bank's points too (ADR 0019). An
// exchange account has no statements: its API reports snapshots (ADR 0018).
const (
	KindCreditCard = "credit_card"
	KindDeposit    = "deposit"
	KindSecurities = "securities"
	KindExchange   = "exchange"
	KindPoints     = "points"
)

// ErrInvalidResponse means the parser returned something that breaks the contract; retrying won't help.
var ErrInvalidResponse = errors.New("invalid parser response")

var kinds = map[parserv1.TransactionKind]string{
	parserv1.TransactionKind_TRANSACTION_KIND_PURCHASE:                "purchase",
	parserv1.TransactionKind_TRANSACTION_KIND_FOREIGN_TRANSACTION_FEE: "foreign_transaction_fee",
	parserv1.TransactionKind_TRANSACTION_KIND_ANNUAL_FEE:              "annual_fee",
	parserv1.TransactionKind_TRANSACTION_KIND_REFUND:                  "refund",
	parserv1.TransactionKind_TRANSACTION_KIND_REBATE:                  "rebate",
	parserv1.TransactionKind_TRANSACTION_KIND_PAYMENT:                 "payment",
}

var pointsKinds = map[parserv1.PointsLineKind]string{
	parserv1.PointsLineKind_POINTS_LINE_KIND_EARN:   "earn",
	parserv1.PointsLineKind_POINTS_LINE_KIND_ADJUST: "adjust",
	parserv1.PointsLineKind_POINTS_LINE_KIND_REDEEM: "redeem",
}

var depositKinds = map[parserv1.DepositLineKind]string{
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_OTHER:    "other",
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_INTEREST: "interest",
	parserv1.DepositLineKind_DEPOSIT_LINE_KIND_FEE:      "fee",
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
// transaction, and returns the document's new status.
func Store(ctx context.Context, pool *pgxpool.Pool, documentID int64, response *parserv1.ParseDocumentResponse) (string, error) {
	result, err := convertResponse(documentID, response)
	if err != nil {
		return "", err
	}
	statement := result.statement

	var outcome string
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		// Before deleting anything: relinking (below, and in a statement stored at the same time)
		// updates every linked line, so taking the lock only then would let each side hold rows the
		// other waits for.
		if err := queries.LockLinks(ctx); err != nil {
			return fmt.Errorf("lock links: %w", err)
		}
		previous, err := queries.DeleteStatementsForDocument(ctx, documentID)
		if err != nil {
			return err
		}
		// A re-parse keeps the statement's id: its page and the links to it stay where they were.
		if len(previous) > 0 {
			statement.ID = &previous[0]
		}

		finish := dbq.FinishDocumentParams{
			ID:            documentID,
			Parser:        &response.Parser,
			ParserVersion: &response.ParserVersion,
			Warnings:      append(append([]string{}, response.GetWarnings()...), result.unbalanced...),
		}

		existing, err := queries.FindStatementByKey(ctx, dbq.FindStatementByKeyParams{
			Institution: statement.Institution,
			Kind:        statement.Kind,
			PeriodStart: statement.PeriodStart,
			ClosingDate: statement.ClosingDate,
		})
		switch {
		case err == nil:
			// Another document already holds this statement (an e-mail and the same one forwarded).
			reason := fmt.Sprintf("same statement as document %d", existing.DocumentID)
			finish.Status, finish.Error, outcome = StatusDuplicate, &reason, StatusDuplicate
			if err := queries.FinishDocument(ctx, finish); err != nil {
				return err
			}
			// A statement this document had before may have been a parent's.
			_, err := relink(ctx, queries)
			return err
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		statementID, err := queries.InsertStatement(ctx, statement)
		if err != nil {
			return err
		}
		var lines []dbq.InsertTransactionsParams
		var accountIDs []int64
		var unitIDs []map[string]int64
		for _, reported := range result.accounts {
			// Found, or created the first time a statement reports it (ADR 0013).
			accountID, err := queries.UpsertAccount(ctx, reported.account)
			if err != nil {
				return fmt.Errorf("find account: %w", err)
			}
			accountIDs = append(accountIDs, accountID)
			// Likewise its units (ADR 0020).
			units, err := reported.unitIDs(ctx, queries)
			if err != nil {
				return err
			}
			unitIDs = append(unitIDs, units)
			for _, line := range reported.lines {
				line.StatementID, line.AccountID, line.UnitID = statementID, accountID, units[line.unit]
				lines = append(lines, line.InsertTransactionsParams)
			}
			for _, balance := range reported.balances {
				balance.StatementID, balance.AccountID, balance.UnitID = statementID, accountID, units[balance.unit]
				if err := queries.InsertBalance(ctx, balance.InsertBalanceParams); err != nil {
					return fmt.Errorf("record balance: %w", err)
				}
			}
			for _, expiry := range reported.expiries {
				expiry.StatementID, expiry.AccountID, expiry.UnitID = statementID, accountID, units[expiry.unit]
				if err := queries.InsertExpiry(ctx, expiry.InsertExpiryParams); err != nil {
					return fmt.Errorf("record expiry: %w", err)
				}
			}
		}
		if _, err := queries.InsertTransactions(ctx, lines); err != nil {
			return err
		}
		if statement.Kind == KindDeposit {
			this := span{id: statementID, documentID: documentID, start: statement.PeriodStart.Time, end: statement.ClosingDate.Time}
			for i, reported := range result.accounts {
				// Each section reports one currency, and always its closing balance.
				if err := trimOverlaps(ctx, queries, this, accountIDs[i], unitIDs[i][reported.balances[0].unit]); err != nil {
					return err
				}
			}
		}
		if statement.Kind == KindSecurities {
			if err := storeHoldings(ctx, queries, statementID, result.trades, accountIDs); err != nil {
				return err
			}
		}
		// Its lines may be parents or children of lines on other statements.
		if _, err := relink(ctx, queries); err != nil {
			return err
		}

		outcome = StatusParsed
		if !statement.Balanced {
			outcome = StatusNeedsReview
		}
		finish.Status = outcome
		return queries.FinishDocument(ctx, finish)
	})
	if err != nil {
		return "", fmt.Errorf("store parse result: %w", err)
	}
	return outcome, nil
}

func convertResponse(documentID int64, response *parserv1.ParseDocumentResponse) (stored, error) {
	bill, deposit, securities := response.GetStatement(), response.GetDepositStatement(), response.GetSecuritiesStatement()
	switch {
	case bill != nil && deposit == nil && securities == nil:
		statement, lines, err := convert(documentID, bill)
		if err != nil {
			return stored{}, err
		}
		// A bank's card lines belong to its credit_card account, which holds no balance: what the bill
		// says is owed stays on the bill (ADR 0013).
		card := storedAccount{account: dbq.UpsertAccountParams{Institution: statement.Institution, Kind: KindCreditCard}}
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
			points.account = dbq.UpsertAccountParams{Institution: statement.Institution, Kind: KindPoints}
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
		Kind:        KindCreditCard,
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
		Kind:        KindDeposit,
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
			Institution: statement.Institution, Kind: KindDeposit, Number: a.GetNumber(), Label: optionalString(a.GetName()),
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
