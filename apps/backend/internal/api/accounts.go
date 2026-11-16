package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/notify"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// The accounts' standing is the domain's (domain.Assess); these handlers read what it needs and show
// what it says.

func (s *Server) ListAccounts(ctx context.Context, _ ListAccountsRequestObject) (ListAccountsResponseObject, error) {
	accounts, err := readAccounts(ctx, dbq.New(s.pool), nil)
	if err != nil {
		return nil, err
	}
	return ListAccounts200JSONResponse(accounts), nil
}

func (s *Server) GetAccount(ctx context.Context, request GetAccountRequestObject) (GetAccountResponseObject, error) {
	queries := dbq.New(s.pool)
	found, err := readAccounts(ctx, queries, &request.Id)
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
		Label: account.Label, Name: account.Name, FullNumber: account.FullNumber, CheckAgainOn: account.CheckAgainOn, Balances: account.Balances,
		Continuity: account.Continuity, Worth: account.Worth, WorthFrom: account.WorthFrom, Valuation: account.Valuation,
		Gaps: account.Gaps, Expiries: account.Expiries, Lots: account.Lots, Lapsed: account.Lapsed, Debits: account.Debits, History: history, Fills: fills,
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

// readAccounts reads the owner's accounts, or the one with id, as they stand today (ledger.ReadAccounts).
func readAccounts(ctx context.Context, queries *dbq.Queries, id *int64) ([]Account, error) {
	now, err := ledger.ReadAccounts(ctx, queries, id)
	if err != nil {
		return nil, err
	}
	today := taipei.Today(time.Now())
	accounts := make([]Account, 0, len(now))
	for _, a := range now {
		accounts = append(accounts, toAccount(a, today))
	}
	return accounts, nil
}

// toAccount shows an account as the domain assesses it (domain.Assess) on today.
func toAccount(a ledger.AccountNow, today time.Time) Account {
	standing := a.Standing
	shown := make(map[string]Balance, len(a.Latest))
	for _, b := range a.Latest {
		shown[b.Unit] = toLatestBalance(b)
	}
	side := Asset
	if a.Kind.Liability() {
		side = Liability
	}
	fresh := a.Fresh(today)
	account := Account{
		Id: a.ID, Institution: a.Institution, Kind: AccountKind(a.Kind), Side: side, Number: a.Number,
		Label: a.Label, Name: a.Name, FullNumber: a.FullNumber, CheckAgainOn: optionalDate(a.CheckAgainOn),
		Continuity: AccountContinuity(standing.Continuity), Balances: make([]Balance, 0, len(standing.Held)),
		Gaps: make([]BalanceGap, 0, len(a.Gaps.Unseen)), Expiries: make([]Expiry, 0, len(a.Expiries)),
		Overdue: fresh.Overdue, Excused: fresh.Excused, FreshFor: a.Kind.FreshFor(),
	}
	for _, b := range standing.Held {
		account.Balances = append(account.Balances, shown[b.Unit])
	}
	for _, g := range a.Gaps.Unseen {
		account.Gaps = append(account.Gaps, BalanceGap{
			Unit: g.Unit, From: openapi_types.Date{Time: g.From}, To: openapi_types.Date{Time: g.To},
			Unseen: numeric.Exact(g.Unseen),
		})
	}
	if v := a.Valuation; v != nil {
		valuation := Valuation{Unit: v.Unit, AsOf: openapi_types.Date{Time: v.AsOf.Time}, Amount: numeric.String(v.Amount)}
		if v.Worth.Valid {
			worth := numeric.String(v.Worth)
			valuation.Worth = &worth
		}
		account.Valuation = &valuation
	}
	if standing.Worth != nil {
		account.Worth, account.WorthFrom = ptr(numeric.Cents(standing.Worth)), ptr(WorthFrom(standing.WorthFrom))
	}
	for _, e := range a.Expiries {
		account.Expiries = append(account.Expiries, toExpiry(e))
	}
	lots, lapsed := pointsLots(a.Programs)
	if len(lots) > 0 {
		account.Lots = &lots
	}
	if len(lapsed) > 0 {
		account.Lapsed = &lapsed
	}
	if next := a.NextLapse(today); next != nil {
		account.NextLapse = ptr(toNextLapse(notify.LapsingOf(*next, today)))
	}
	if len(a.Debits) > 0 {
		debits := make([]Debit, 0, len(a.Debits))
		for _, b := range a.Debits {
			debits = append(debits, toDebit(notify.DebitOf(b)))
		}
		account.Debits = &debits
	}
	if shortfalls := domain.Shortfalls(standing.Held, a.Debits, fresh, today); len(shortfalls) > 0 {
		shown := make([]Shortfall, 0, len(shortfalls))
		for _, s := range shortfalls {
			shown = append(shown, toShortfall(s))
		}
		account.Shortfalls = &shown
	}
	return account
}

// pointsLots shows an account's points programs' lots, a day's together, soonest first, and what lapsed,
// oldest first.
func pointsLots(programs []ledger.ProgramLots) ([]PointsLot, []LapsedPoints) {
	var lots []PointsLot
	var lapsed []LapsedPoints
	for _, p := range programs {
		var days []domain.Lot
		for _, l := range p.Held {
			if n := len(days); n > 0 && days[n-1].By.Equal(l.By) && days[n-1].Unaged == l.Unaged {
				days[n-1].Amount = new(big.Rat).Add(days[n-1].Amount, l.Amount)
				continue
			}
			days = append(days, l)
		}
		for _, l := range days {
			lot := PointsLot{Unit: p.Unit, By: openapi_types.Date{Time: l.By}, Amount: numeric.Exact(l.Amount)}
			if l.Unaged {
				lot.Unaged = &l.Unaged
			}
			lots = append(lots, lot)
		}
		for _, l := range p.Lapsed {
			lapsed = append(lapsed, LapsedPoints{
				Unit: p.Unit, From: openapi_types.Date{Time: l.From}, To: openapi_types.Date{Time: l.To}, Amount: numeric.Exact(l.Amount),
			})
		}
	}
	slices.SortStableFunc(lots, func(a, b PointsLot) int { return a.By.Compare(b.By.Time) })
	slices.SortStableFunc(lapsed, func(a, b LapsedPoints) int { return a.To.Compare(b.To.Time) })
	return lots, lapsed
}

// toShortfall shows a shortfall as notifications store it (notify.Facts), so the two read alike.
func toShortfall(s domain.Shortfall) Shortfall {
	shown := Shortfall{
		Unit: s.Unit, AsOf: openapi_types.Date{Time: s.AsOf}, Balance: numeric.Exact(s.Balance),
		Owed: numeric.Exact(s.Owed), Lacks: numeric.Exact(s.Lacks), Debits: make([]Debit, 0, len(s.Debits)),
	}
	for _, b := range s.Debits {
		shown.Debits = append(shown.Debits, toDebit(notify.DebitOf(b)))
	}
	return shown
}

func toDebit(d notify.Debit) Debit {
	debit := Debit{StatementId: d.StatementID, Institution: d.Institution, DueDate: dateOf(d.DueDate), Unit: d.Unit, Amount: d.Amount}
	if d.PaidOn != "" {
		debit.PaidOn = ptr(dateOf(d.PaidOn))
	}
	return debit
}

func toNextLapse(l notify.Lapsing) NextLapse {
	return NextLapse{Unit: l.Unit, Amount: l.Amount, By: dateOf(l.By), Estimated: l.Estimated, Soon: l.Soon}
}

// dateOf reads a date notify wrote.
func dateOf(day string) openapi_types.Date {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		panic(fmt.Sprintf("notify wrote a date as %q", day))
	}
	return openapi_types.Date{Time: t}
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
