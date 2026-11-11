package ledger

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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

var (
	// ErrNoAccount means no account the owner has goes by the id.
	ErrNoAccount = errors.New("no such account")
	// ErrNameTaken means another account goes by the name: no two do (ADR 0019).
	ErrNameTaken = errors.New("an account goes by that name already")
	// ErrNumberMismatch means a full number is not the one the bank masks (domain.FullNumber).
	ErrNumberMismatch = errors.New("the full number does not fit the one the bank prints")
	// ErrListedTwice means a unit is in a snapshot twice, however it is spelled.
	ErrListedTwice = errors.New("listed twice")
	// ErrNegative means the owner gave an amount below 0: what an account holds, or owes, is recorded as a
	// positive number, and the account's side gives the sign (ADR 0022). A negative principal would count
	// a loan as an asset.
	ErrNegative = errors.New("negative amount")
	// ErrNotKeptByHand means the account has a source other than the owner.
	ErrNotKeptByHand = errors.New("only an account the owner opened is kept by hand")
)

// Problem is what the owner asked for that the ledger refuses, said in words for them; it is one of the
// errors above.
type Problem struct {
	Kind    error
	Message string
}

func (p *Problem) Error() string { return p.Message }
func (p *Problem) Unwrap() error { return p.Kind }

// Explain is the words for a Problem in err, if it holds one.
func Explain(err error) (string, bool) {
	var p *Problem
	if errors.As(err, &p) {
		return p.Message, true
	}
	return "", false
}

func refuse(kind error, format string, args ...any) error {
	return &Problem{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// OpenAccount opens an account the owner keeps by hand (ADR 0019), of any kind a source could report but
// a card's: the name they open it with is its own.
func OpenAccount(ctx context.Context, queries *dbq.Queries, kind domain.AccountKind, name string) (int64, error) {
	label := strings.TrimSpace(name)
	id, err := queries.CreateOwnerAccount(ctx, dbq.CreateOwnerAccountParams{Kind: kind, Label: &label})
	if nameTaken(err) {
		return 0, refuse(ErrNameTaken, "an account named %s exists already", label)
	}
	if err != nil {
		return 0, fmt.Errorf("open account: %w", err)
	}
	return id, nil
}

// AccountDetails are what the owner gives an account (ADR 0016): nil leaves one as it is, blank clears it.
type AccountDetails struct {
	Name, FullNumber *string
}

// UpdateAccount keeps the owner's details of an account and, since a full number fits counterparties
// (ADR 0016), works the links out again with it in the same transaction.
func UpdateAccount(ctx context.Context, pool *pgxpool.Pool, id int64, details AccountDetails) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		account, err := queries.GetAccount(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoAccount
		}
		if err != nil {
			return fmt.Errorf("get account: %w", err)
		}

		params := dbq.UpdateAccountDetailsParams{ID: id}
		taken := account.Label
		if name := details.Name; name != nil {
			params.SetName = true
			if trimmed := strings.TrimSpace(*name); trimmed != "" {
				params.Name, taken = &trimmed, &trimmed
			}
		}
		if given := details.FullNumber; given != nil {
			params.SetFullNumber = true
			if strings.NewReplacer(" ", "", "-", "").Replace(*given) != "" {
				full, ok := domain.FullNumber(account.Number, *given)
				if !ok {
					return refuse(ErrNumberMismatch, "%s does not fit %s: the full number has its digits, and the same ones where the bank prints them", *given, account.Number)
				}
				params.FullNumber = &full
			}
		}
		if _, err := queries.UpdateAccountDetails(ctx, params); nameTaken(err) {
			// Cleared, the account goes by its own name again, which another may have taken meanwhile.
			return refuse(ErrNameTaken, "an account named %s exists already", deref(taken))
		} else if err != nil {
			return fmt.Errorf("update account: %w", err)
		}
		if params.SetFullNumber {
			if _, err := Relink(ctx, queries); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

// uniqueViolation is Postgres's SQLSTATE for a row a unique index already has.
const uniqueViolation = "23505"

// nameTaken reports whether an error is another account going by the name (accounts_name_key).
func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "accounts_name_key"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Holding is what the owner says an account they keep holds of a unit, and what of it will lapse.
type Holding struct {
	Symbol   string
	Amount   *big.Rat
	Expiries []Expiry
}

// Expiry is an amount of a unit that will lapse by a day (ADR 0019).
type Expiry struct {
	Amount *big.Rat
	By     time.Time
}

// KeepHoldings keeps what the owner says an account they keep holds on a day: a snapshot of the whole
// account, as an exchange's (StoreSnapshot), and what will lapse in place of what they said before.
func KeepHoldings(ctx context.Context, pool *pgxpool.Pool, accountID int64, day time.Time, holdings []Holding) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		source, err := queries.GetAccountSource(ctx, accountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoAccount
		}
		if err != nil {
			return fmt.Errorf("get account: %w", err)
		}
		if source.Institution != domain.OwnerInstitution {
			return refuse(ErrNotKeptByHand, "only an account the owner opened is kept by hand")
		}
		held := make(map[string]*big.Rat, len(holdings))
		for _, h := range holdings {
			symbol := domain.Symbol(source.Kind.UnitKind(), h.Symbol)
			if _, twice := held[symbol]; twice {
				return refuse(ErrListedTwice, "%s is listed twice", symbol)
			}
			if h.Amount.Sign() < 0 {
				what := "held"
				if source.Kind.Liability() {
					what = "owed"
				}
				return refuse(ErrNegative, "%s: record what is %s as a positive number", symbol, what)
			}
			for _, e := range h.Expiries {
				if e.Amount.Sign() <= 0 {
					return refuse(ErrNegative, "%s: what lapses is a positive number", symbol)
				}
			}
			held[symbol] = h.Amount
		}
		units, err := StoreSnapshot(ctx, queries, Snapshot{AccountID: accountID, Kind: source.Kind, Day: day, Holdings: held})
		if err != nil {
			return err
		}
		if err := queries.ClearOwnerExpiries(ctx, accountID); err != nil {
			return fmt.Errorf("clear expiries: %w", err)
		}
		for _, h := range holdings {
			unitID, ok := units[domain.Symbol(source.Kind.UnitKind(), h.Symbol)]
			if !ok {
				// Nothing held lapses.
				continue
			}
			for _, e := range h.Expiries {
				amount, err := numeric.FromRat(e.Amount)
				if err != nil {
					return fmt.Errorf("expiry of %s: %w", h.Symbol, err)
				}
				if err := queries.InsertOwnerExpiry(ctx, dbq.InsertOwnerExpiryParams{
					AccountID: accountID, UnitID: unitID, Amount: amount, LapsesBy: pgtype.Date{Time: e.By, Valid: true},
				}); err != nil {
					return fmt.Errorf("insert expiry: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("keep holdings: %w", err)
	}
	return nil
}
