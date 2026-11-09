package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	exact "github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Accounts the owner keeps by hand (ADR 0019): of any kind a source could report but a card's, their
// source the owner. A supplement, never what a feature depends on.

// ownerInstitution is the institution of an account the owner opened: they are its only source.
const ownerInstitution = "owner"

func (s *Server) CreateAccount(ctx context.Context, request CreateAccountRequestObject) (CreateAccountResponseObject, error) {
	name := strings.TrimSpace(request.Body.Name)
	if request.Body.Kind == AccountKindCreditCard {
		return CreateAccount400JSONResponse{ProblemJSONResponse{Message: "a card account holds only its bank's bills' lines; none is kept by hand"}}, nil
	}
	if name == "" {
		return CreateAccount400JSONResponse{ProblemJSONResponse{Message: "an account kept by hand needs a name"}}, nil
	}
	id, err := dbq.New(s.pool).CreateOwnerAccount(ctx, dbq.CreateOwnerAccountParams{Kind: string(request.Body.Kind), Label: &name})
	if nameTaken(err) {
		return CreateAccount409JSONResponse{Message: fmt.Sprintf("an account named %s exists already", name)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	return CreateAccount201JSONResponse{Id: id}, nil
}

// holding is one unit of what the owner reports an account holds.
type holding struct {
	unit     string
	amount   pgtype.Numeric
	expiries []dbq.InsertOwnerExpiryParams
}

func (s *Server) UpdateHoldings(ctx context.Context, request UpdateHoldingsRequestObject) (UpdateHoldingsResponseObject, error) {
	source, err := dbq.New(s.pool).GetAccountSource(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateHoldings404JSONResponse(notFound("account", request.Id)), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if source.Institution != ownerInstitution {
		return UpdateHoldings400JSONResponse{ProblemJSONResponse{Message: "only an account the owner opened is kept by hand"}}, nil
	}
	kind := ingest.UnitKind(source.Kind)
	holdings, problem := readHoldings(request.Id, kind, *request.Body)
	if problem != "" {
		return UpdateHoldings400JSONResponse{ProblemJSONResponse{Message: problem}}, nil
	}

	// A snapshot of the whole account on the day, as an exchange's (ADR 0018): a later one the same day
	// replaces it, and a unit it held before and no longer lists is at 0.
	today := pgtype.Date{Time: taipei.Today(time.Now()), Valid: true}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		symbols := make([]string, 0, len(holdings))
		for _, h := range holdings {
			symbols = append(symbols, h.unit)
		}
		units, err := ingest.Units(ctx, queries, kind, symbols...)
		if err != nil {
			return err
		}
		if err := queries.ClearSnapshot(ctx, dbq.ClearSnapshotParams{AccountID: request.Id, AsOf: today}); err != nil {
			return fmt.Errorf("clear snapshot: %w", err)
		}
		held := dbq.InsertSnapshotParams{AccountID: request.Id, AsOf: today}
		for _, h := range holdings {
			held.UnitIds = append(held.UnitIds, units[h.unit])
			held.Amounts = append(held.Amounts, h.amount)
		}
		if len(holdings) > 0 {
			if err := queries.InsertSnapshot(ctx, held); err != nil {
				return fmt.Errorf("insert snapshot: %w", err)
			}
		}
		if err := queries.ImplySoldOut(ctx, dbq.ImplySoldOutParams{AccountID: request.Id, AsOf: today}); err != nil {
			return fmt.Errorf("imply sold out: %w", err)
		}
		if err := queries.ClearOwnerExpiries(ctx, request.Id); err != nil {
			return fmt.Errorf("clear expiries: %w", err)
		}
		for _, h := range holdings {
			for _, e := range h.expiries {
				e.UnitID = units[h.unit]
				if err := queries.InsertOwnerExpiry(ctx, e); err != nil {
					return fmt.Errorf("insert expiry: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update holdings: %w", err)
	}
	return UpdateHoldings204Response{}, nil
}

// readHoldings checks what the owner sent, each unit once, its symbol as sources spell one of its kind
// (money upper case, ADR 0020); a problem is said in words.
func readHoldings(accountID int64, kind string, sent HoldingsUpdate) ([]holding, string) {
	holdings := make([]holding, 0, len(sent.Holdings))
	seen := map[string]bool{}
	for _, h := range sent.Holdings {
		unit := strings.TrimSpace(h.Unit)
		if kind == ingest.UnitMoney {
			unit = strings.ToUpper(unit)
		}
		if unit == "" {
			return nil, "every holding needs a unit"
		}
		if seen[unit] {
			return nil, fmt.Sprintf("%s is listed twice", unit)
		}
		seen[unit] = true
		amount, ok := decimalValue(h.Amount)
		if !ok {
			return nil, fmt.Sprintf("%q is no number", h.Amount)
		}
		held := holding{unit: unit, amount: amount}
		for _, e := range h.Expiries {
			lapsing, ok := decimalValue(e.Amount)
			if !ok {
				return nil, fmt.Sprintf("%q is no number", e.Amount)
			}
			held.expiries = append(held.expiries, dbq.InsertOwnerExpiryParams{
				AccountID: accountID, Amount: lapsing, LapsesBy: pgtype.Date{Time: e.By.Time, Valid: true},
			})
		}
		holdings = append(holdings, held)
	}
	return holdings, ""
}

// uniqueViolation is Postgres's SQLSTATE for a row a unique index already has.
const uniqueViolation = "23505"

// nameTaken reports whether an error is another account going by the name the owner gave: no two do.
func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "accounts_name_key"
}

// decimalValue reads an amount the owner typed, exactly (ADR 0004); thousands separators allowed.
func decimalValue(text Decimal) (pgtype.Numeric, bool) {
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(strings.TrimSpace(text), ",", ""))
	if !ok {
		return pgtype.Numeric{}, false
	}
	n, err := exact.FromRat(r)
	return n, err == nil
}
