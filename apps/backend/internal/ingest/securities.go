package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/genproto/googleapis/type/decimal"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
)

// convertSecurities takes a brokerage account's monthly statement (ADR 0017): each trade is a line in
// the security's unit, its shares bought positive and sold negative, and its money a row of trades;
// each holding is a balance on the holdings date, on which the statement closes.
func convertSecurities(documentID int64, s *parserv1.SecuritiesStatement) (stored, error) {
	var errs []error
	rec := s.GetReconciliation()
	statement := dbq.InsertStatementParams{
		DocumentID:  documentID,
		Institution: s.GetInstitution(),
		Kind:        KindSecurities,
		Balanced:    rec.GetBalanced(),
	}
	if statement.Institution == "" || s.GetAccountNumber() == "" {
		errs = append(errs, errors.New("institution and account number are required"))
	}
	if rec == nil {
		errs = append(errs, errors.New("reconciliation is missing"))
	}
	statement.PeriodStart, errs = requiredDate(s.GetPeriodStart(), "period start", errs)
	statement.ClosingDate, errs = requiredDate(s.GetHoldingsDate(), "holdings date", errs)

	reported := storedAccount{account: dbq.UpsertAccountParams{
		Institution: statement.Institution, Kind: KindSecurities, Number: s.GetAccountNumber(),
	}}
	held := map[string]bool{}
	for i, h := range s.GetHoldings() {
		where := fmt.Sprintf("holding %d", i)
		switch {
		case h.GetName() == "":
			errs = append(errs, fmt.Errorf("%s: no name", where))
		case held[h.GetName()]:
			errs = append(errs, fmt.Errorf("%s: %s listed twice", where, h.GetName()))
		}
		held[h.GetName()] = true
		balance := dbq.InsertBalanceParams{Unit: h.GetName(), AsOf: statement.ClosingDate}
		balance.Amount, errs = requiredNumeric(h.GetShares(), where+" shares", errs)
		reported.balances = append(reported.balances, balance)
	}

	result := stored{}
	for i, t := range s.GetTrades() {
		at := fmt.Sprintf("trade %d", i)
		line := dbq.InsertTransactionsParams{LineNo: int32(i), Unit: t.GetName(), Description: t.GetName()}
		shares := t.GetShares()
		switch t.GetSide() {
		case parserv1.TradeSide_TRADE_SIDE_BUY:
			line.Kind = "buy"
		case parserv1.TradeSide_TRADE_SIDE_SELL:
			// Shares leave the account.
			line.Kind = "sell"
			if shares != nil {
				shares = &decimal.Decimal{Value: "-" + shares.GetValue()}
			}
		default:
			errs = append(errs, fmt.Errorf("%s: side %s", at, t.GetSide()))
		}
		if t.GetName() == "" || strings.HasPrefix(t.GetShares().GetValue(), "-") {
			errs = append(errs, fmt.Errorf("%s: a name and positive shares are required", at))
		}
		line.TransactionDate, errs = requiredDate(t.GetDate(), at+" date", errs)
		line.Amount, errs = requiredNumeric(shares, at+" shares", errs)
		reported.lines = append(reported.lines, line)

		var price, amount, fee, tax, net pgtype.Numeric
		price, errs = requiredNumeric(t.GetPrice(), at+" price", errs)
		amount, errs = requiredNumeric(t.GetAmount(), at+" amount", errs)
		fee, errs = requiredNumeric(t.GetFee(), at+" fee", errs)
		tax, errs = requiredNumeric(t.GetTax(), at+" tax", errs)
		net, errs = requiredNumeric(t.GetNet(), at+" net", errs)
		prepaid, err := numeric(t.GetPrepaid())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s prepaid: %w", at, err))
		}
		trades := &result.trades
		trades.LineNos = append(trades.LineNos, int32(i))
		trades.Categories = append(trades.Categories, t.GetCategory())
		trades.Prices, trades.Amounts, trades.Fees = append(trades.Prices, price), append(trades.Amounts, amount), append(trades.Fees, fee)
		trades.Taxes, trades.Nets, trades.Prepaids = append(trades.Taxes, tax), append(trades.Nets, net), append(trades.Prepaids, prepaid)
	}

	if rec != nil && !rec.GetBalanced() {
		result.unbalanced = append(result.unbalanced, fmt.Sprintf("%s %s: the trades' %s come to %s, the statement prints %s",
			statement.Institution, s.GetAccountNumber(), rec.GetStatedLabel(), rec.GetComputed().GetValue(), rec.GetStated().GetValue()))
	}
	if len(errs) > 0 {
		return stored{}, fmt.Errorf("%w: %w", ErrInvalidResponse, errors.Join(errs...))
	}
	result.statement = statement
	result.accounts = []storedAccount{reported}
	return result, nil
}

// storeHoldings adds a securities statement's trades' money, then works out again which securities
// each of its accounts held none of on each statement's day: a statement's holdings are the whole
// account, so what it does not list is at 0 (ADR 0017). Like links, derived from everything stored,
// since statements arrive in any order.
func storeHoldings(ctx context.Context, queries *dbq.Queries, statementID int64, trades dbq.InsertTradesParams, accountIDs []int64) error {
	if len(trades.LineNos) > 0 {
		trades.StatementID = statementID
		if err := queries.InsertTrades(ctx, trades); err != nil {
			return fmt.Errorf("store trades: %w", err)
		}
	}
	for _, id := range accountIDs {
		if err := queries.ClearEmptyHoldings(ctx, id); err != nil {
			return fmt.Errorf("clear empty holdings: %w", err)
		}
		if err := queries.ImplyEmptyHoldings(ctx, id); err != nil {
			return fmt.Errorf("imply empty holdings: %w", err)
		}
	}
	return nil
}
