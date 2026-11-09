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
	"time"

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
	valuations, err := queries.LatestValuations(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest valuations: %w", err)
	}

	expiries, err := queries.LatestExpiries(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest expiries: %w", err)
	}

	valuationOf := map[int64]*dbq.LatestValuationsRow{}
	for i := range valuations {
		valuationOf[valuations[i].AccountID] = &valuations[i]
	}
	expiriesOf := map[int64][]Expiry{}
	for _, e := range expiries {
		expiriesOf[e.AccountID] = append(expiriesOf[e.AccountID], toExpiry(e))
	}
	balancesOf := map[int64][]Balance{}
	for _, b := range latest {
		balancesOf[b.AccountID] = append(balancesOf[b.AccountID], toLatestBalance(b))
	}
	gapsOf := map[int64][]BalanceGap{}
	unshown := map[int64]bool{}
	for _, g := range gaps {
		if !g.Itemized {
			unshown[g.AccountID] = true
			continue
		}
		gapsOf[g.AccountID] = append(gapsOf[g.AccountID], toGap(g))
	}
	accounts := make(ListAccounts200JSONResponse, 0, len(rows))
	for _, row := range rows {
		account := toAccount(dbq.GetAccountRow(row), balancesOf[row.ID], gapsOf[row.ID], unshown[row.ID], valuationOf[row.ID])
		account.Expiries = append(account.Expiries, expiriesOf[row.ID]...)
		accounts = append(accounts, account)
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
	valuations, err := queries.LatestValuations(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest valuations: %w", err)
	}
	expiryRows, err := queries.LatestExpiries(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest expiries: %w", err)
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
	var valuation *dbq.LatestValuationsRow
	for i := range valuations {
		if valuations[i].AccountID == request.Id {
			valuation = &valuations[i]
		}
	}
	gaps := make([]BalanceGap, 0, len(gapRows))
	unshown := false
	for _, g := range gapRows {
		if !g.Itemized {
			unshown = true
			continue
		}
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
	account := toAccount(row, latest, gaps, unshown, valuation)
	for _, e := range expiryRows {
		if e.AccountID == request.Id {
			account.Expiries = append(account.Expiries, toExpiry(e))
		}
	}
	return GetAccount200JSONResponse(AccountDetail{
		Id: account.Id, Institution: account.Institution, Kind: account.Kind, Number: account.Number,
		Label: account.Label, Name: account.Name, FullNumber: account.FullNumber, Balances: account.Balances,
		Continuity: account.Continuity, Worth: account.Worth, WorthFrom: account.WorthFrom, Valuation: account.Valuation,
		Gaps: account.Gaps, Expiries: account.Expiries, History: history, Positions: held,
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

// toAccount takes the account's gaps, and whether its balance moved in a unit no statement has listed a
// line of, as one whose statements print only its balance: nothing could show that move, so the account
// cannot be checked (ADR 0013). An exchange reports what it holds and nothing else (ADR 0018).
func toAccount(row dbq.GetAccountRow, balances []Balance, gaps []BalanceGap, unshown bool, valuation *dbq.LatestValuationsRow) Account {
	// Only what statements report has lines to join balances with: snapshots alone, an exchange's or the
	// owner's, cannot be checked (ADR 0018, ADR 0019).
	reportedByStatements := slices.ContainsFunc(balances, func(b Balance) bool { return b.StatementId != nil })
	continuity := Unknown
	checkable := row.Kind == ingest.KindDeposit || row.Kind == ingest.KindSecurities || row.Kind == ingest.KindPoints
	if checkable && reportedByStatements && !unshown {
		continuity = Checked
	}
	newest := newestDay(balances)
	reported := len(balances) > 0
	if row.Kind == ingest.KindSecurities || row.Kind == ingest.KindExchange || row.Kind == ingest.KindPoints {
		// A security, a coin, or a program at 0 is one the account no longer holds (ADR 0017, 0018, 0019).
		balances = slices.DeleteFunc(balances, func(b Balance) bool { return isZero(b.Amount) })
	}
	if balances == nil {
		balances = []Balance{}
	}
	if gaps == nil {
		gaps = []BalanceGap{}
	}
	account := Account{
		Id: row.ID, Institution: row.Institution, Kind: AccountKind(row.Kind), Number: row.Number,
		Label: row.Label, Name: row.Name, FullNumber: row.FullNumber, Balances: balances, Continuity: continuity,
		Gaps: gaps, Expiries: []Expiry{},
	}
	if valuation != nil {
		v := toValuation(*valuation)
		account.Valuation = &v
	}
	account.Worth, account.WorthFrom = worthOf(balances, account.Valuation, newest)
	if reported && len(balances) == 0 {
		// Its sources say it holds nothing at all.
		account.Worth, account.WorthFrom = ptr("0.00"), ptr(WorthFromHoldings)
	}
	return account
}

func toExpiry(e dbq.LatestExpiriesRow) Expiry {
	return Expiry{Unit: e.Unit, Amount: decimalString(e.Amount), By: openapi_types.Date{Time: e.LapsesBy.Time}}
}

// newestDay is the day of the newest of the account's balances, zero ones included: a snapshot lists a
// unit sold out at 0 on its day (ADR 0018).
func newestDay(balances []Balance) time.Time {
	var newest time.Time
	for _, b := range balances {
		if b.AsOf.After(newest) {
			newest = b.AsOf.Time
		}
	}
	return newest
}

func toValuation(v dbq.LatestValuationsRow) Valuation {
	valuation := Valuation{Unit: v.Unit, AsOf: openapi_types.Date{Time: v.AsOf.Time}, Amount: decimalString(v.Amount)}
	if v.Worth.Valid {
		worth := decimalString(v.Worth)
		valuation.Worth = &worth
	}
	return valuation
}

// toLatestBalance adds the balance's unit, its newest price, and its worth in NT$ at that price (ADR 0020).
func toLatestBalance(b dbq.LatestBalancesRow) Balance {
	balance := toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID)
	balance.UnitId = &b.UnitID
	balance.Code = b.Code
	if b.Worth.Valid {
		worth := decimalString(b.Worth)
		balance.Worth = &worth
	}
	// NT$ is worth itself, with no price of its own.
	if b.PriceSource != nil && b.PriceQuote != nil {
		price := Price{
			AsOf: b.PricedAt.Time, Source: PriceSource(*b.PriceSource), Quote: *b.PriceQuote,
			Quoted: decimalString(b.PriceQuoted),
		}
		if b.PriceTwd.Valid {
			twd := decimalString(b.PriceTwd)
			price.Twd = &twd
		}
		balance.Price = &price
	}
	return balance
}

func isZero(value Decimal) bool {
	n, ok := new(big.Rat).SetString(value)
	return ok && n.Sign() == 0
}

// worthOf is an account's worth (ADR 0018): its balances' worth added up exactly; else, when one of them
// has none, its valuation on the day of its newest balance; else none.
func worthOf(balances []Balance, valuation *Valuation, newest time.Time) (*Decimal, *WorthFrom) {
	if worth := holdingsWorth(balances); worth != nil {
		return worth, ptr(WorthFromHoldings)
	}
	if valuation == nil || valuation.Worth == nil || !valuation.AsOf.Equal(newest) {
		return nil, nil
	}
	worth, ok := new(big.Rat).SetString(*valuation.Worth)
	if !ok {
		return nil, nil
	}
	return ptr(worth.FloatString(2)), ptr(WorthFromValuation)
}

// holdingsWorth adds up balances' worth exactly; none when a balance has no worth, or there are none.
func holdingsWorth(balances []Balance) *Decimal {
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

func ptr[T any](v T) *T { return &v }

// toBalance takes the statement that reported the balance; a snapshot has none (ADR 0018).
func toBalance(unit string, asOf pgtype.Date, amount pgtype.Numeric, statementID *int64) Balance {
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
		switch digits := strings.NewReplacer(" ", "", "-", "").Replace(*full); {
		case digits == "":
		case row.Number == "":
			// No source prints a number of the account to fit, such as one the owner keeps (ADR 0019).
			trimmed := strings.TrimSpace(*full)
			params.FullNumber = &trimmed
		case !fitsMasked(row.Number, digits):
			return UpdateAccount400JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf(
				"%s does not fit %s: the full number has its digits, and the same ones where the bank prints them", *full, row.Number)}}, nil
		default:
			params.FullNumber = &digits
		}
	}
	if _, err := queries.UpdateAccountDetails(ctx, params); nameTaken(err) {
		// Cleared, the account goes by its own name again, which another may have taken meanwhile.
		taken := deref(row.Label)
		if params.Name != nil {
			taken = *params.Name
		}
		return UpdateAccount409JSONResponse(ProblemJSONResponse{Message: fmt.Sprintf("an account named %s exists already", taken)}), nil
	} else if err != nil {
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
