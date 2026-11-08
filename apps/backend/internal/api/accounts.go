package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
)

// Continuity is worked out on every read from the balances and lines (ADR 0013): cheap at a person's
// volume, and never stale whatever order statements arrive in.

func (s *Server) ListAccounts(ctx context.Context, _ ListAccountsRequestObject) (ListAccountsResponseObject, error) {
	queries := dbq.New(s.pool)
	rows, err := queries.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	latest, err := queries.LatestBalances(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest balances: %w", err)
	}
	gaps, err := queries.AccountGaps(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("account gaps: %w", err)
	}

	balancesOf := map[int64][]Balance{}
	for _, b := range latest {
		balancesOf[b.AccountID] = append(balancesOf[b.AccountID], toLatestBalance(b))
	}
	gapsOf := map[int64][]BalanceGap{}
	for _, g := range gaps {
		gapsOf[g.AccountID] = append(gapsOf[g.AccountID], toGap(g))
	}
	accounts := make(ListAccounts200JSONResponse, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, toAccount(dbq.GetAccountRow(row), balancesOf[row.ID], gapsOf[row.ID]))
	}
	return accounts, nil
}

func (s *Server) GetAccount(ctx context.Context, request GetAccountRequestObject) (GetAccountResponseObject, error) {
	queries := dbq.New(s.pool)
	row, err := queries.GetAccount(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return GetAccount404JSONResponse{notFound("account", request.Id)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	rows, err := queries.AccountBalances(ctx, request.Id)
	if err != nil {
		return nil, fmt.Errorf("account balances: %w", err)
	}
	latestRows, err := queries.LatestBalances(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest balances: %w", err)
	}
	gapRows, err := queries.AccountGaps(ctx, &request.Id)
	if err != nil {
		return nil, fmt.Errorf("account gaps: %w", err)
	}

	history := make([]Balance, 0, len(rows))
	for _, b := range rows {
		history = append(history, toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID))
	}
	var latest []Balance
	for _, b := range latestRows {
		if b.AccountID == request.Id {
			latest = append(latest, toLatestBalance(b))
		}
	}
	gaps := make([]BalanceGap, 0, len(gapRows))
	for _, g := range gapRows {
		gaps = append(gaps, toGap(g))
	}
	var held *[]Position
	if row.Kind == ingest.KindSecurities {
		trades, err := queries.AccountTrades(ctx, request.Id)
		if err != nil {
			return nil, fmt.Errorf("account trades: %w", err)
		}
		list := positions(trades, rows, gapRows, latest)
		held = &list
	}
	account := toAccount(row, latest, gaps)
	return GetAccount200JSONResponse(AccountDetail{
		Id: account.Id, Institution: account.Institution, Kind: account.Kind, Number: account.Number,
		Label: account.Label, Name: account.Name, FullNumber: account.FullNumber, Balances: account.Balances,
		Continuity: account.Continuity, Worth: account.Worth, Gaps: account.Gaps, History: history, Positions: held,
	}), nil
}

func (s *Server) ListAccountLines(ctx context.Context, request ListAccountLinesRequestObject) (ListAccountLinesResponseObject, error) {
	queries := dbq.New(s.pool)
	if _, err := queries.GetAccount(ctx, request.Id); errors.Is(err, pgx.ErrNoRows) {
		return ListAccountLines404JSONResponse{notFound("account", request.Id)}, nil
	} else if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	rows, err := queries.AccountLines(ctx, dbq.AccountLinesParams{
		AccountID: request.Id,
		RowLimit:  limit(request.Params.Limit),
		RowOffset: clamp(request.Params.Offset, 0, 0, math.MaxInt32),
	})
	if err != nil {
		return nil, fmt.Errorf("account lines: %w", err)
	}
	lines := make(ListAccountLines200JSONResponse, 0, len(rows))
	for _, r := range rows {
		line := AccountLine{
			Id: r.ID, StatementId: r.StatementID, Date: openapi_types.Date{Time: r.TransactionDate.Time},
			Description: r.Description, Memo: r.Memo, Counterparty: r.Counterparty, Kind: r.Kind, Unit: r.Unit,
			Amount: decimalString(r.Amount),
		}
		if r.Balance.Valid {
			balance := decimalString(r.Balance)
			line.Balance = &balance
		}
		line.Counterpart = lineCounterpart(r.CounterpartID, r.CounterpartStatementID, r.CounterpartDate, r.CounterpartAccountID,
			r.CounterpartAccountKind, r.CounterpartInstitution, r.CounterpartAccountName,
			cmp.Or(r.CounterpartFullNumber, r.CounterpartAccountNumber))
		line.Trade = tradeOf(r.TradeCategory, r.TradePrice, r.TradeAmount, r.TradeFee, r.TradeTax, r.TradeNet, r.TradePrepaid)
		lines = append(lines, line)
	}
	return lines, nil
}

func toAccount(row dbq.GetAccountRow, balances []Balance, gaps []BalanceGap) Account {
	continuity := Unknown
	if row.Kind == ingest.KindDeposit || row.Kind == ingest.KindSecurities {
		continuity = Checked
	}
	if row.Kind == ingest.KindSecurities {
		// A security at 0 is one the account no longer holds (ADR 0017).
		balances = slices.DeleteFunc(balances, func(b Balance) bool { return isZero(b.Amount) })
	}
	if balances == nil {
		balances = []Balance{}
	}
	if gaps == nil {
		gaps = []BalanceGap{}
	}
	return Account{
		Id: row.ID, Institution: row.Institution, Kind: AccountKind(row.Kind), Number: row.Number,
		Label: row.Label, Name: row.Name, FullNumber: row.FullNumber, Balances: balances, Continuity: continuity,
		Gaps: gaps, Worth: worthOf(balances),
	}
}

// toLatestBalance adds the balance's worth in NT$ at the newest rate of its unit (ADR 0015).
func toLatestBalance(b dbq.LatestBalancesRow) Balance {
	balance := toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID)
	if b.Worth.Valid {
		worth := decimalString(b.Worth)
		balance.Worth = &worth
	}
	if b.RateDay.Valid && b.Unit != "TWD" {
		balance.RateDay = &openapi_types.Date{Time: b.RateDay.Time}
	}
	if b.Code != "" {
		balance.Code = &b.Code
	}
	if b.Price.Valid {
		price := decimalString(b.Price)
		balance.Price = &price
	}
	return balance
}

func isZero(value Decimal) bool {
	n, ok := new(big.Rat).SetString(value)
	return ok && n.Sign() == 0
}

// worthOf adds up balances' worth exactly; none when a balance has no worth, or there are none.
func worthOf(balances []Balance) *Decimal {
	if len(balances) == 0 {
		return nil
	}
	total := new(big.Rat)
	for _, b := range balances {
		if b.Worth == nil {
			return nil
		}
		value, ok := new(big.Rat).SetString(*b.Worth)
		if !ok {
			return nil
		}
		total.Add(total, value)
	}
	worth := total.FloatString(2)
	return &worth
}

func toBalance(unit string, asOf pgtype.Date, amount pgtype.Numeric, statementID int64) Balance {
	return Balance{Unit: unit, AsOf: openapi_types.Date{Time: asOf.Time}, Amount: decimalString(amount), StatementId: statementID}
}

func toGap(g dbq.AccountGapsRow) BalanceGap {
	return BalanceGap{
		Unit: g.Unit, From: openapi_types.Date{Time: g.FromDate.Time}, To: openapi_types.Date{Time: g.ToDate.Time},
		Unseen: decimalString(g.Unseen),
	}
}

func (s *Server) UpdateAccount(ctx context.Context, request UpdateAccountRequestObject) (UpdateAccountResponseObject, error) {
	queries := dbq.New(s.pool)
	row, err := queries.GetAccount(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateAccount404JSONResponse(notFound("account", request.Id)), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	params := dbq.UpdateAccountDetailsParams{ID: request.Id}
	if name := request.Body.Name; name != nil {
		params.SetName = true
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			params.Name = &trimmed
		}
	}
	if full := request.Body.FullNumber; full != nil {
		params.SetFullNumber = true
		digits := strings.NewReplacer(" ", "", "-", "").Replace(*full)
		if digits != "" {
			if !fitsMasked(row.Number, digits) {
				return UpdateAccount400JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf(
					"%s does not fit %s: the full number has its digits, and the same ones where the bank prints them", *full, row.Number)}}, nil
			}
			params.FullNumber = &digits
		}
	}
	if _, err := queries.UpdateAccountDetails(ctx, params); err != nil {
		return nil, fmt.Errorf("update account: %w", err)
	}
	// A full number fits counterparties (ADR 0016): work the links out again with it.
	if params.SetFullNumber {
		if _, err := ingest.Relink(ctx, s.pool); err != nil {
			return nil, err
		}
	}
	return UpdateAccount204Response{}, nil
}

// fitsMasked reports whether a full number is the one a bank masks as `masked`: as long, all digits,
// and the same digit wherever the bank prints one.
func fitsMasked(masked, full string) bool {
	if len(full) != len(masked) {
		return false
	}
	for i := range len(full) {
		if full[i] < '0' || full[i] > '9' || (masked[i] != '*' && masked[i] != full[i]) {
			return false
		}
	}
	return true
}
