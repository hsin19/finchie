package numeric

import (
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestADecimalKeepsEveryDigitWhateverItsNotation(t *testing.T) {
	for in, want := range map[string]string{
		"4.0260888e-7": "0.00000040260888",
		"0.030503548":  "0.030503548",
		"1e3":          "1000",
		"-12.50":       "-12.5",
	} {
		r, _ := new(big.Rat).SetString(in)
		n, err := FromRat(r)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got, _ := n.Value(); got != want {
			t.Fatalf("%s = %v, want %s", in, got, want)
		}
	}
	if _, err := FromRat(big.NewRat(1, 3)); err == nil {
		t.Fatal("a third has no decimal: want an error")
	}
}

func TestANumericReadsBackExactlyWhateverItsScale(t *testing.T) {
	for _, in := range []string{"780.00", "-6553", "0.00000040260888", "12000", "-0.5"} {
		var n pgtype.Numeric
		if err := n.Scan(in); err != nil {
			t.Fatal(err)
		}
		want, _ := new(big.Rat).SetString(in)
		if got := ToRat(n); got == nil || got.Cmp(want) != 0 {
			t.Fatalf("%s read as %v", in, got)
		}
	}
	if ToRat(pgtype.Numeric{}) != nil {
		t.Fatal("NULL read as a number")
	}
}

func TestAddingKeepsTheFinerScale(t *testing.T) {
	a, _ := Parse("1.5")
	b, _ := Parse("2,000.25")
	if got := String(Add(a, b)); got != "2001.75" {
		t.Fatalf("1.5 + 2,000.25 = %s", got)
	}
	if got := String(Add(a, pgtype.Numeric{})); got != "1.5" {
		t.Fatalf("1.5 + NULL = %s", got)
	}
}
