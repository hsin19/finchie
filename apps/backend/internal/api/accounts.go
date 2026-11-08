package api

import (
	"context"
	"errors"
	"fmt"
	"math"

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
		balancesOf[b.AccountID] = append(balancesOf[b.AccountID], toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID))
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
	gapRows, err := queries.AccountGaps(ctx, &request.Id)
	if err != nil {
		return nil, fmt.Errorf("account gaps: %w", err)
	}

	history := make([]Balance, 0, len(rows))
	var latest []Balance
	seen := map[string]bool{}
	for _, b := range rows {
		balance := toBalance(b.Unit, b.AsOf, b.Amount, b.StatementID)
		history = append(history, balance)
		// Newest first, so the first of each unit is its latest.
		if !seen[b.Unit] {
			seen[b.Unit] = true
			latest = append(latest, balance)
		}
	}
	gaps := make([]BalanceGap, 0, len(gapRows))
	for _, g := range gapRows {
		gaps = append(gaps, toGap(g))
	}
	account := toAccount(row, latest, gaps)
	return GetAccount200JSONResponse(AccountDetail{
		Id: account.Id, Institution: account.Institution, Kind: account.Kind, Number: account.Number,
		Label: account.Label, Name: account.Name, Balances: account.Balances, Continuity: account.Continuity,
		Gaps: account.Gaps, History: history,
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
		lines = append(lines, line)
	}
	return lines, nil
}

func toAccount(row dbq.GetAccountRow, balances []Balance, gaps []BalanceGap) Account {
	continuity := Unknown
	if row.Kind == ingest.KindDeposit {
		continuity = Checked
	}
	if balances == nil {
		balances = []Balance{}
	}
	if gaps == nil {
		gaps = []BalanceGap{}
	}
	return Account{
		Id: row.ID, Institution: row.Institution, Kind: AccountKind(row.Kind), Number: row.Number,
		Label: row.Label, Name: row.Name, Balances: balances, Continuity: continuity, Gaps: gaps,
	}
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
