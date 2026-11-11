package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

// Units and prices (ADR 0020). The owner may price any unit but the base currency; their price counts
// while it is the unit's newest, like any source's.

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
	// As typed, thousands separators aside.
	value := strings.ReplaceAll(strings.TrimSpace(request.Body.Value), ",", "")
	quote := domain.BaseCurrency
	if q := request.Body.Quote; q != nil && strings.TrimSpace(*q) != "" {
		quote = *q
	}
	err := ledger.SetOwnerPrice(ctx, dbq.New(s.pool), request.Id, value, quote, time.Now())
	switch {
	case errors.Is(err, ledger.ErrNoUnit):
		return SetUnitPrice404JSONResponse(notFound("unit", request.Id)), nil
	case errors.Is(err, ledger.ErrNoPrice):
		return SetUnitPrice400JSONResponse{ProblemJSONResponse{Message: explain(err)}}, nil
	case err != nil:
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
