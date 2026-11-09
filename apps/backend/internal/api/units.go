package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
)

// Units and prices (ADR 0020). The owner may price any unit; their price counts while it is the unit's
// newest, like any source's.

// ownerSource names the prices the owner gives.
const ownerSource = "owner"

func (s *Server) ListUnits(ctx context.Context, request ListUnitsRequestObject) (ListUnitsResponseObject, error) {
	rows, err := dbq.New(s.pool).ListUnits(ctx, (*string)(request.Params.Kind))
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	units := make(ListUnits200JSONResponse, 0, len(rows))
	for _, r := range rows {
		units = append(units, Unit{Id: r.ID, Kind: UnitKind(r.Kind), Symbol: r.Symbol, Code: r.Code})
	}
	return units, nil
}

func (s *Server) SetUnitPrice(ctx context.Context, request SetUnitPriceRequestObject) (SetUnitPriceResponseObject, error) {
	queries := dbq.New(s.pool)
	unit, err := queries.GetUnit(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetUnitPrice404JSONResponse(notFound("unit", request.Id)), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get unit: %w", err)
	}
	value, ok := decimalValue(request.Body.Value)
	if !ok || value.Int.Sign() < 0 {
		return SetUnitPrice400JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("%q is no price: a number, 0 or more", request.Body.Value)}}, nil
	}
	quote := "TWD"
	if q := request.Body.Quote; q != nil && strings.TrimSpace(*q) != "" {
		quote = strings.ToUpper(strings.TrimSpace(*q))
	}
	if unit.Kind == ingest.UnitMoney && unit.Symbol == quote {
		return SetUnitPrice400JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("%s is not priced in itself", quote)}}, nil
	}
	quotes, err := queries.ListUnits(ctx, ptr(ingest.UnitMoney))
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	if !slicesContainsSymbol(quotes, quote) {
		return SetUnitPrice400JSONResponse{ProblemJSONResponse{Message: fmt.Sprintf("%s is no money unit any account holds", quote)}}, nil
	}
	err = queries.UpsertPrices(ctx, dbq.UpsertPricesParams{
		Source: ownerSource, Kind: unit.Kind, Symbols: []string{unit.Symbol},
		AsOf:    []pgtype.Timestamptz{{Time: time.Now(), Valid: true}},
		Amounts: []pgtype.Numeric{{Int: big.NewInt(1), Valid: true}}, Quotes: []string{quote}, QuoteAmounts: []pgtype.Numeric{value},
	})
	if err != nil {
		return nil, fmt.Errorf("set price: %w", err)
	}
	return SetUnitPrice204Response{}, nil
}

func (s *Server) ClearUnitPrice(ctx context.Context, request ClearUnitPriceRequestObject) (ClearUnitPriceResponseObject, error) {
	queries := dbq.New(s.pool)
	if _, err := queries.GetUnit(ctx, request.Id); errors.Is(err, pgx.ErrNoRows) {
		return ClearUnitPrice404JSONResponse{notFound("unit", request.Id)}, nil
	} else if err != nil {
		return nil, fmt.Errorf("get unit: %w", err)
	}
	if err := queries.ClearOwnerPrices(ctx, request.Id); err != nil {
		return nil, fmt.Errorf("clear prices: %w", err)
	}
	return ClearUnitPrice204Response{}, nil
}

func slicesContainsSymbol(units []dbq.ListUnitsRow, symbol string) bool {
	for _, u := range units {
		if u.Symbol == symbol {
			return true
		}
	}
	return false
}
