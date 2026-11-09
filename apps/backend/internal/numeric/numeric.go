// Package numeric writes exact rationals as NUMERIC values (ADR 0004).
package numeric

import (
	"fmt"
	"math/big"

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
