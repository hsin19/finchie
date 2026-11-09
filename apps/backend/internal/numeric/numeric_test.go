package numeric

import (
	"math/big"
	"testing"
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
