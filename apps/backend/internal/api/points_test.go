package api

import (
	"fmt"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

func TestAProgramsLotsOfOneDayAreShownTogetherSoonestFirst(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	lot := func(by string, n int64, unaged bool) domain.Lot {
		return domain.Lot{By: day(by), Amount: big.NewRat(n, 1), Unaged: unaged}
	}
	lotsOf, _ := pointsLots([]ledger.ProgramLots{
		{AccountID: 1, Unit: "台新Point(信用卡)", Lots: domain.Lots{Held: []domain.Lot{
			lot("2028-01-31", 40, true), lot("2028-01-31", 1013, false), lot("2028-01-31", 567, false), lot("2029-01-31", 644, false),
		}}},
		{AccountID: 1, Unit: "亞洲萬里通里數", Lots: domain.Lots{Held: []domain.Lot{lot("2027-06-30", 900, false)}}},
	})
	var got []string
	for _, l := range lotsOf[1] {
		got = append(got, fmt.Sprintf("%s %s %s %v", l.Unit, l.By, l.Amount, l.Unaged != nil))
	}
	// The unaged stay apart: their day is only the latest they lapse by.
	want := []string{
		"亞洲萬里通里數 2027-06-30 900 false",
		"台新Point(信用卡) 2028-01-31 40 true",
		"台新Point(信用卡) 2028-01-31 1580 false",
		"台新Point(信用卡) 2029-01-31 644 false",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("lots =\n%v\nwant\n%v", got, want)
	}
}
