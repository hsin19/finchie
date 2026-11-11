package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Accounts the owner keeps by hand (ADR 0019) and the owner's details of any account (ADR 0016). The rules
// are the ledger's; these handlers read what was sent and say what went wrong.

func (s *Server) CreateAccount(ctx context.Context, request CreateAccountRequestObject) (CreateAccountResponseObject, error) {
	kind := domain.AccountKind(request.Body.Kind)
	if !kind.KeptByHand() {
		return CreateAccount400JSONResponse{ProblemJSONResponse{Message: "a card account holds only its bank's bills' lines; none is kept by hand"}}, nil
	}
	if strings.TrimSpace(request.Body.Name) == "" {
		return CreateAccount400JSONResponse{ProblemJSONResponse{Message: "an account kept by hand needs a name"}}, nil
	}
	id, err := ledger.OpenAccount(ctx, dbq.New(s.pool), kind, request.Body.Name)
	if errors.Is(err, ledger.ErrNameTaken) {
		return CreateAccount409JSONResponse{Message: explain(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateAccount201JSONResponse{Id: id}, nil
}

func (s *Server) UpdateAccount(ctx context.Context, request UpdateAccountRequestObject) (UpdateAccountResponseObject, error) {
	err := ledger.UpdateAccount(ctx, s.pool, request.Id, ledger.AccountDetails{Name: request.Body.Name, FullNumber: request.Body.FullNumber})
	switch {
	case errors.Is(err, ledger.ErrNoAccount):
		return UpdateAccount404JSONResponse(notFound("account", request.Id)), nil
	case errors.Is(err, ledger.ErrNumberMismatch):
		return UpdateAccount400JSONResponse{ProblemJSONResponse{Message: explain(err)}}, nil
	case errors.Is(err, ledger.ErrNameTaken):
		return UpdateAccount409JSONResponse(ProblemJSONResponse{Message: explain(err)}), nil
	case err != nil:
		return nil, err
	}
	return UpdateAccount204Response{}, nil
}

func (s *Server) UpdateHoldings(ctx context.Context, request UpdateHoldingsRequestObject) (UpdateHoldingsResponseObject, error) {
	holdings, problem := readHoldings(*request.Body)
	if problem != "" {
		return UpdateHoldings400JSONResponse{ProblemJSONResponse{Message: problem}}, nil
	}
	err := ledger.KeepHoldings(ctx, s.pool, request.Id, taipei.Today(time.Now()), holdings)
	switch {
	case errors.Is(err, ledger.ErrNoAccount):
		return UpdateHoldings404JSONResponse(notFound("account", request.Id)), nil
	case errors.Is(err, ledger.ErrNotKeptByHand), errors.Is(err, ledger.ErrListedTwice), errors.Is(err, ledger.ErrNegative):
		return UpdateHoldings400JSONResponse{ProblemJSONResponse{Message: explain(err)}}, nil
	case err != nil:
		return nil, fmt.Errorf("update holdings: %w", err)
	}
	return UpdateHoldings204Response{}, nil
}

// readHoldings reads what the owner sent: each holding with a unit, each amount a number (thousands
// separators allowed); a problem is said in words.
func readHoldings(sent HoldingsUpdate) ([]ledger.Holding, string) {
	holdings := make([]ledger.Holding, 0, len(sent.Holdings))
	for _, h := range sent.Holdings {
		unit := strings.TrimSpace(h.Unit)
		if unit == "" {
			return nil, "every holding needs a unit"
		}
		amount, ok := typedNumber(h.Amount)
		if !ok {
			return nil, fmt.Sprintf("%q is no number", h.Amount)
		}
		held := ledger.Holding{Symbol: unit, Amount: amount}
		for _, e := range h.Expiries {
			lapsing, ok := typedNumber(e.Amount)
			if !ok {
				return nil, fmt.Sprintf("%q is no number", e.Amount)
			}
			held.Expiries = append(held.Expiries, ledger.Expiry{Amount: lapsing, By: e.By.Time})
		}
		holdings = append(holdings, held)
	}
	return holdings, ""
}

// typedNumber reads an amount a person typed, exactly (ADR 0004); thousands separators allowed.
func typedNumber(text Decimal) (*big.Rat, bool) {
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(strings.TrimSpace(text), ",", ""))
	return r, ok
}

// explain is the ledger's words for what it refused.
func explain(err error) string {
	if message, ok := ledger.Explain(err); ok {
		return message
	}
	return err.Error()
}
