// Package numeric converts between exact rationals, as the domain computes with them, and NUMERIC values,
// as Postgres stores them (ADR 0004). No amount ever passes through a float.
package numeric

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// maxPlaces bounds the search for a decimal's places; anything read from a decimal has far fewer.
const maxPlaces = 40

// FromRat writes r with all its digits. It is for values read from decimals, in plain or exponent
// notation (a NUMERIC parses neither 4.0260888e-7 nor a fraction), and for their sums and products,
// which are decimals too; anything else, such as a third, is an error.
func FromRat(r *big.Rat) (pgtype.Numeric, error) {
	places := 0
	scaled := new(big.Rat).Set(r)
	ten := big.NewRat(10, 1)
	for !scaled.IsInt() {
		if places == maxPlaces {
			return pgtype.Numeric{}, fmt.Errorf("%s is no decimal", r.String())
		}
		scaled.Mul(scaled, ten)
		places++
	}
	var n pgtype.Numeric
	if err := n.Scan(r.FloatString(places)); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("numeric %s: %w", r.String(), err)
	}
	return n, nil
}

// ToRat reads a NUMERIC exactly; nil for NULL. A stored amount is always finite (ingest refuses NaN and
// infinities), so anything that does not read is NULL too.
func ToRat(n pgtype.Numeric) *big.Rat {
	if !n.Valid || n.NaN || n.InfinityModifier != pgtype.Finite || n.Int == nil {
		return nil
	}
	r := new(big.Rat).SetInt(n.Int)
	if n.Exp == 0 {
		return r
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(absInt32(n.Exp))), nil)
	if n.Exp > 0 {
		return r.Mul(r, new(big.Rat).SetInt(factor))
	}
	return r.Quo(r, new(big.Rat).SetInt(factor))
}

// MustFromRat is FromRat for a value known to be a decimal, such as 0 or a sum of decimals.
func MustFromRat(r *big.Rat) pgtype.Numeric {
	n, err := FromRat(r)
	if err != nil {
		panic(err)
	}
	return n
}

// One is the NUMERIC 1.
func One() pgtype.Numeric { return pgtype.Numeric{Int: big.NewInt(1), Valid: true} }

// String renders a NUMERIC exactly as Postgres stores it ("780.00", "-6553"); empty for NULL.
func String(n pgtype.Numeric) string {
	value, err := n.Value()
	if err != nil || value == nil {
		return ""
	}
	s, _ := value.(string)
	return s
}

// Add adds exactly, keeping the finer of the two scales ("1.5" + "2.25" = "3.75"); NULL adds nothing.
func Add(a, b pgtype.Numeric) pgtype.Numeric {
	if !b.Valid {
		return a
	}
	if !a.Valid {
		return b
	}
	exp := min(a.Exp, b.Exp)
	sum := new(big.Int).Add(scaled(a, exp), scaled(b, exp))
	return pgtype.Numeric{Int: sum, Exp: exp, Valid: true}
}

// Zero is the NUMERIC 0.
func Zero() pgtype.Numeric { return pgtype.Numeric{Int: big.NewInt(0), Valid: true} }

// FromText reads a decimal as a source prints it, keeping its places ("8.00" stays 8.00), in plain or
// exponent notation; NaN and infinities are no decimal.
func FromText(text string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strings.TrimSpace(text)); err == nil && n.Valid && !n.NaN && n.InfinityModifier == pgtype.Finite {
		return n, nil
	}
	r, ok := new(big.Rat).SetString(strings.TrimSpace(text))
	if !ok {
		return pgtype.Numeric{}, fmt.Errorf("%q is no decimal", text)
	}
	return FromRat(r)
}

// Parse reads an amount a person typed, exactly; thousands separators are allowed.
func Parse(text string) (pgtype.Numeric, bool) {
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(strings.TrimSpace(text), ",", ""))
	if !ok {
		return pgtype.Numeric{}, false
	}
	n, err := FromRat(r)
	return n, err == nil
}

// Exact renders a decimal with the places it needs and no more ("100", "-0.5").
func Exact(r *big.Rat) string {
	places := 0
	scaled := new(big.Rat).Set(r)
	for !scaled.IsInt() && places < maxPlaces {
		scaled.Mul(scaled, big.NewRat(10, 1))
		places++
	}
	return r.FloatString(places)
}

// Cents renders r rounded to two places, as worth and cost are shown.
func Cents(r *big.Rat) string { return r.FloatString(2) }

// scaled returns n's digits as an integer count of 10^exp, for exp <= n.Exp.
func scaled(n pgtype.Numeric, exp int32) *big.Int {
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n.Exp-exp)), nil)
	return new(big.Int).Mul(n.Int, factor)
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
