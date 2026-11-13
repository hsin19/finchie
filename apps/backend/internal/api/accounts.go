package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// The accounts' standing is the domain's (domain.Assess); these handlers read what it needs and show
// what it says.

func (s *Server) ListAccounts(ctx context.Context, _ ListAccountsRequestObject) (ListAccountsResponseObject, error) {
	accounts, _, err := readAccounts(ctx, dbq.New(s.pool), nil)
	if err != nil {
		return nil, err
	}
	return ListAccounts200JSONResponse(accounts), nil
}

func (s *Server) GetAccount(ctx context.Context, request GetAccountRequestObject) (GetAccountResponseObject, error) {
	queries := dbq.New(s.pool)
	found, _, err := readAccounts(ctx, queries, &request.Id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return GetAccount404JSONResponse{notFound("account", request.Id)}, nil
	}
	account := found[0]

	rows, err := queries.AccountBalances(ctx, request.Id)
	if err != nil {
		return nil, fmt.Errorf("account balances: %w", err)
	}
	history := make([]Balance, 0, len(rows))
	for _, b := range rows {
		history = append(history, toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID))
	}
	fills, err := gapFills(ctx, queries, request.Id)
	if err != nil {
		return nil, err
	}
	detail := AccountDetail{
		Id: account.Id, Institution: account.Institution, Kind: account.Kind, Side: account.Side, Number: account.Number,
		Label: account.Label, Name: account.Name, FullNumber: account.FullNumber, Balances: account.Balances,
		Continuity: account.Continuity, Worth: account.Worth, WorthFrom: account.WorthFrom, Valuation: account.Valuation,
		Gaps: account.Gaps, Expiries: account.Expiries, History: history, Fills: fills,
	}
	if domain.AccountKind(account.Kind) == domain.Securities {
		held, err := readPositions(ctx, queries, request.Id, rows, account.Balances)
		if err != nil {
			return nil, err
		}
		detail.Positions = &held
	}
	return GetAccount200JSONResponse(detail), nil
}

// readAccounts reads the owner's accounts, or the one with id, and how each stands: as shown, and what
// each is worth to the domain.
func readAccounts(ctx context.Context, queries *dbq.Queries, id *int64) ([]Account, []domain.Worth, error) {
	rows, err := queries.ListAccounts(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list accounts: %w", err)
	}
	latest, err := queries.LatestBalances(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("latest balances: %w", err)
	}
	gaps, err := ledger.Gaps(ctx, queries, id)
	if err != nil {
		return nil, nil, err
	}
	valuations, err := queries.LatestValuations(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("latest valuations: %w", err)
	}
	expiries, err := queries.LatestExpiries(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("latest expiries: %w", err)
	}

	balancesOf := map[int64][]dbq.LatestBalancesRow{}
	for _, b := range latest {
		balancesOf[b.AccountID] = append(balancesOf[b.AccountID], b)
	}
	valuationOf := map[int64]dbq.LatestValuationsRow{}
	for _, v := range valuations {
		valuationOf[v.AccountID] = v
	}
	expiriesOf := map[int64][]Expiry{}
	for _, e := range expiries {
		expiriesOf[e.AccountID] = append(expiriesOf[e.AccountID], toExpiry(e))
	}
	gapsOf := domain.ReadGaps(gaps)

	accounts := make([]Account, 0, len(rows))
	worths := make([]domain.Worth, 0, len(rows))
	for _, row := range rows {
		var valuation *dbq.LatestValuationsRow
		if v, ok := valuationOf[row.ID]; ok {
			valuation = &v
		}
		account, standing := toAccount(row, balancesOf[row.ID], gapsOf[row.ID], valuation)
		account.Expiries = append(account.Expiries, expiriesOf[row.ID]...)
		accounts = append(accounts, account)
		worths = append(worths, domain.Worth{Kind: row.Kind, Worth: standing.Worth})
	}
	return accounts, worths, nil
}

// toAccount shows an account as the domain assesses it (domain.Assess).
func toAccount(row dbq.ListAccountsRow, latest []dbq.LatestBalancesRow, gaps domain.Gaps, valuation *dbq.LatestValuationsRow) (Account, domain.Standing) {
	balances := make([]domain.Balance, 0, len(latest))
	shown := make(map[string]Balance, len(latest))
	for _, b := range latest {
		balances = append(balances, domain.Balance{
			Unit: b.Unit, AsOf: b.AsOf.Time, Amount: numeric.ToRat(b.Amount), Worth: numeric.ToRat(b.Worth),
			FromStatement: b.StatementID != nil,
		})
		shown[b.Unit] = toLatestBalance(b)
	}
	var valued *domain.Valuation
	if valuation != nil {
		valued = &domain.Valuation{AsOf: valuation.AsOf.Time, Worth: numeric.ToRat(valuation.Worth)}
	}
	standing := domain.Assess(row.Kind, balances, gaps, valued)

	side := Asset
	if row.Kind.Liability() {
		side = Liability
	}
	account := Account{
		Id: row.ID, Institution: row.Institution, Kind: AccountKind(row.Kind), Side: side, Number: row.Number,
		Label: row.Label, Name: row.Name, FullNumber: row.FullNumber, Continuity: AccountContinuity(standing.Continuity),
		Balances: make([]Balance, 0, len(standing.Held)), Gaps: make([]BalanceGap, 0, len(gaps.Unseen)), Expiries: []Expiry{},
	}
	for _, b := range standing.Held {
		account.Balances = append(account.Balances, shown[b.Unit])
	}
	for _, g := range gaps.Unseen {
		account.Gaps = append(account.Gaps, BalanceGap{
			Unit: g.Unit, From: openapi_types.Date{Time: g.From}, To: openapi_types.Date{Time: g.To},
			Unseen: numeric.Exact(g.Unseen),
		})
	}
	if valuation != nil {
		v := Valuation{Unit: valuation.Unit, AsOf: openapi_types.Date{Time: valuation.AsOf.Time}, Amount: numeric.String(valuation.Amount)}
		if valuation.Worth.Valid {
			worth := numeric.String(valuation.Worth)
			v.Worth = &worth
		}
		account.Valuation = &v
	}
	if standing.Worth != nil {
		account.Worth, account.WorthFrom = ptr(numeric.Cents(standing.Worth)), ptr(WorthFrom(standing.WorthFrom))
	}
	return account, standing
}

// readPositions works out a securities account's positions (domain.Positions) from its balances' history,
// gaps, and trades.
func readPositions(ctx context.Context, queries *dbq.Queries, accountID int64, history []dbq.AccountBalancesRow, latest []Balance) ([]Position, error) {
	trades, err := queries.AccountTrades(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account trades: %w", err)
	}
	gaps, err := ledger.Gaps(ctx, queries, &accountID)
	if err != nil {
		return nil, err
	}

	holdings := map[string][]domain.Holding{}
	for _, b := range history {
		holdings[b.Unit] = append(holdings[b.Unit], domain.Holding{Day: b.AsOf.Time, Shares: ratOrZero(b.Amount)})
	}
	gapsTo := map[string][]time.Time{}
	for _, g := range gaps {
		gapsTo[g.Unit] = append(gapsTo[g.Unit], g.To)
	}
	traded := map[string][]domain.Trade{}
	for _, t := range trades {
		shares := ratOrZero(t.Shares)
		traded[t.Unit] = append(traded[t.Unit], domain.Trade{
			Day: t.TransactionDate.Time, Sell: t.Kind == domain.Sell, Shares: shares.Abs(shares),
			Amount: ratOrZero(t.Amount), Fee: ratOrZero(t.Fee), Tax: ratOrZero(t.Tax),
		})
	}
	newest := map[string]Balance{}
	worth := map[string]*big.Rat{}
	for _, b := range latest {
		newest[b.Unit] = b
		if b.Worth != nil {
			if w, ok := new(big.Rat).SetString(*b.Worth); ok {
				worth[b.Unit] = w
			}
		}
	}

	positions := domain.Positions(holdings, gapsTo, traded, worth)
	result := make([]Position, 0, len(positions))
	for _, p := range positions {
		position := Position{Unit: p.Unit, Shares: numeric.Exact(p.Shares), Worth: newest[p.Unit].Worth, Code: newest[p.Unit].Code}
		if p.Since != nil {
			position.Since = &openapi_types.Date{Time: *p.Since}
			position.Cost = ptr(numeric.Cents(p.Cost))
			if p.Realized != nil {
				position.Realized = ptr(numeric.Cents(p.Realized))
			}
			if p.Gain != nil {
				position.Gain = ptr(numeric.Cents(p.Gain))
			}
		}
		result = append(result, position)
	}
	return result, nil
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
			Amount: numeric.String(r.Amount),
		}
		if domain.LineKind(r.Kind) == domain.Fill {
			line.Fill = &LineFill{Id: r.ID, From: openapi_types.Date{Time: r.FillFrom.Time}, To: openapi_types.Date{Time: r.FillTo.Time}}
		}
		if r.Balance.Valid {
			line.Balance = ptr(numeric.String(r.Balance))
		}
		line.Counterpart = lineCounterpart(r.CounterpartID, r.CounterpartStatementID, r.CounterpartDate, r.CounterpartAccountID,
			r.CounterpartAccountKind, r.CounterpartInstitution, r.CounterpartAccountName,
			cmp.Or(r.CounterpartFullNumber, r.CounterpartAccountNumber))
		line.Trade = tradeOf(r.TradeCategory, r.TradePrice, r.TradeAmount, r.TradeFee, r.TradeTax, r.TradeNet, r.TradePrepaid)
		lines = append(lines, line)
	}
	return lines, nil
}

func toExpiry(e dbq.LatestExpiriesRow) Expiry {
	return Expiry{Unit: e.Unit, Amount: numeric.String(e.Amount), By: openapi_types.Date{Time: e.LapsesBy.Time}}
}

// toLatestBalance adds the balance's unit, its newest price, and its worth in NT$ at that price (ADR 0020).
func toLatestBalance(b dbq.LatestBalancesRow) Balance {
	balance := toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID)
	balance.UnitId = &b.UnitID
	balance.Code = b.Code
	if b.Worth.Valid {
		balance.Worth = ptr(numeric.String(b.Worth))
	}
	// The base currency is worth itself, with no price of its own.
	if b.PriceSource != nil && b.PriceQuote != nil {
		price := Price{
			AsOf: b.PricedAt.Time, Source: PriceSource(*b.PriceSource), Quote: *b.PriceQuote,
			Quoted: numeric.String(b.PriceQuoted),
		}
		if b.PriceTwd.Valid {
			price.Twd = ptr(numeric.String(b.PriceTwd))
		}
		balance.Price = &price
	}
	return balance
}

// toBalance takes the statement that reported the balance; a snapshot has none (ADR 0018).
func toBalance(unit string, asOf pgtype.Date, amount pgtype.Numeric, statementID *int64) Balance {
	return Balance{Unit: unit, AsOf: openapi_types.Date{Time: asOf.Time}, Amount: numeric.String(amount), StatementId: statementID}
}

func ratOrZero(n pgtype.Numeric) *big.Rat {
	if r := numeric.ToRat(n); r != nil {
		return r
	}
	return new(big.Rat)
}

func ptr[T any](v T) *T { return &v }
