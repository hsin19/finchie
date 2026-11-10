package api

import (
	"context"
	"fmt"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

func (s *Server) GetNetWorth(ctx context.Context, _ GetNetWorthRequestObject) (GetNetWorthResponseObject, error) {
	queries := dbq.New(s.pool)
	_, worths, err := readAccounts(ctx, queries, nil)
	if err != nil {
		return nil, err
	}
	rows, err := queries.CardBillsOwed(ctx)
	if err != nil {
		return nil, fmt.Errorf("card bills: %w", err)
	}
	bills := make([]domain.Bill, 0, len(rows))
	for _, r := range rows {
		bill := domain.Bill{ID: r.ID, Institution: r.Institution, Closing: r.ClosingDate.Time, Currency: deref(r.Currency), TotalDue: numeric.ToRat(r.TotalDue)}
		if r.PaidOn.Valid {
			bill.PaidOn = &r.PaidOn.Time
		}
		bills = append(bills, bill)
	}
	owed := domain.Owed(bills)
	net := domain.Net(worths, owed)

	result := NetWorth{
		Holdings: numeric.Cents(net.Holdings), Unvalued: net.Unvalued, Owed: numeric.Cents(net.Owed),
		OwedElsewhere: net.OwedElsewhere, Net: numeric.Cents(net.Net), UnpaidBills: make([]int64, 0, len(owed)),
		ByKind: make([]KindWorth, 0, len(net.ByKind)),
	}
	for _, k := range net.ByKind {
		result.ByKind = append(result.ByKind, KindWorth{Kind: AccountKind(k.Kind), Unvalued: k.Unvalued, Worth: numeric.Cents(k.Worth)})
	}
	for _, b := range owed {
		result.UnpaidBills = append(result.UnpaidBills, b.ID)
	}
	return GetNetWorth200JSONResponse(result), nil
}
