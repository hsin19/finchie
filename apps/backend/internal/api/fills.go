package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// What the owner makes up for a gap whose reports are lost: a line with no statement. The rules are the
// ledger's and the database's (counted_lines); these handlers read what was sent and say what went wrong.

func (s *Server) FillGap(ctx context.Context, request FillGapRequestObject) (FillGapResponseObject, error) {
	queries := dbq.New(s.pool)
	if _, err := queries.GetAccountSource(ctx, request.Id); errors.Is(err, pgx.ErrNoRows) {
		return FillGap404JSONResponse{notFound("account", request.Id)}, nil
	} else if err != nil {
		return nil, fmt.Errorf("get account %d: %w", request.Id, err)
	}
	body := request.Body
	fill, err := ledger.FillGap(ctx, s.pool, request.Id, body.Unit, body.From.Time, body.To.Time, body.Memo)
	if errors.Is(err, ledger.ErrNoGap) {
		return FillGap409JSONResponse{Message: fmt.Sprintf(
			"account %d has no gap in %s from %s to %s to fill: its balances join up, it is filled already, or it changed",
			request.Id, body.Unit, body.From, body.To)}, nil
	}
	if err != nil {
		return nil, err
	}
	return FillGap201JSONResponse{
		Id: fill.ID, Unit: fill.Gap.Unit, From: body.From, To: body.To,
		On: openapi_types.Date{Time: fill.Gap.FilledOn()}, Amount: numeric.Exact(fill.Gap.Unseen), Memo: fill.Memo,
	}, nil
}

func (s *Server) UpdateGapFill(ctx context.Context, request UpdateGapFillRequestObject) (UpdateGapFillResponseObject, error) {
	err := ledger.SetFillMemo(ctx, dbq.New(s.pool), request.Id, request.FillId, request.Body.Memo)
	if errors.Is(err, ledger.ErrNoFill) {
		return UpdateGapFill404JSONResponse{notFound("fill", request.FillId)}, nil
	}
	if err != nil {
		return nil, err
	}
	return UpdateGapFill204Response{}, nil
}

func (s *Server) DeleteGapFill(ctx context.Context, request DeleteGapFillRequestObject) (DeleteGapFillResponseObject, error) {
	deleted, err := dbq.New(s.pool).DeleteFillLine(ctx, dbq.DeleteFillLineParams{ID: request.FillId, AccountID: request.Id})
	if err != nil {
		return nil, fmt.Errorf("delete fill %d: %w", request.FillId, err)
	}
	if deleted == 0 {
		return DeleteGapFill404JSONResponse{notFound("fill", request.FillId)}, nil
	}
	return DeleteGapFill204Response{}, nil
}

// gapFills are the owner's fills of an account's gaps that count.
func gapFills(ctx context.Context, queries *dbq.Queries, accountID int64) ([]GapFill, error) {
	rows, err := queries.AccountFills(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account fills: %w", err)
	}
	fills := make([]GapFill, 0, len(rows))
	for _, f := range rows {
		fills = append(fills, GapFill{
			Id: f.ID, Unit: f.Unit, From: openapi_types.Date{Time: f.FromDate.Time}, To: openapi_types.Date{Time: f.ToDate.Time},
			On: openapi_types.Date{Time: f.OnDate.Time}, Amount: numeric.String(f.Amount), Memo: f.Memo,
		})
	}
	return fills, nil
}
