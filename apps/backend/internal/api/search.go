package api

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
)

// searchFilter is one search's filters, read once from the request; each query that lists or totals the
// lines takes its parameters from it, so the list, its totals, and its facets apply the same filters
// (line_matches, migration 00017).
type searchFilter struct {
	from, to, closingFrom, closingTo pgtype.Date
	institution, card, query         *string
	account                          *int64
	accountKind                      *string
	overseas, hideTransfers          bool
	kinds                            []string
	// Fees fold into their purchases when purchases are listed; asked for fees alone, they are rows.
	fold bool
}

func readSearch(params SearchTransactionsParams) searchFilter {
	f := searchFilter{
		from: date(params.From), to: date(params.To), closingFrom: date(params.ClosingFrom), closingTo: date(params.ClosingTo),
		institution: blankAsNil(params.Institution), card: blankAsNil(params.Card), query: blankAsNil(params.Q),
		account: params.Account, overseas: params.Overseas != nil && *params.Overseas,
		// A transfer between the owner's lines is neither spent nor received (ADR 0016).
		hideTransfers: params.HideTransfers != nil && *params.HideTransfers,
	}
	if params.AccountKind != nil && params.AccountKind.Valid() {
		f.accountKind = ptr(string(*params.AccountKind))
	}
	if params.Kind != nil {
		f.kinds = make([]string, 0, len(*params.Kind))
		for _, kind := range *params.Kind {
			f.kinds = append(f.kinds, string(kind))
		}
	}
	f.fold = f.kinds == nil || slices.Contains(f.kinds, string(domain.Purchase))
	return f
}

func (f searchFilter) search(sort string, limit, offset int32) dbq.SearchTransactionsParams {
	return dbq.SearchTransactionsParams{
		Fold: f.fold, Sort: sort, RowLimit: limit, RowOffset: offset, Kinds: f.kinds,
		Institution: f.institution, CardLast4: f.card, AccountID: f.account,
		FromDate: f.from, ToDate: f.to, ClosingFrom: f.closingFrom, ClosingTo: f.closingTo,
		Overseas: f.overseas, AccountKind: f.accountKind, HideTransfers: f.hideTransfers, Query: f.query,
	}
}

func (f searchFilter) totals() dbq.TransactionTotalsParams {
	return dbq.TransactionTotalsParams{
		Fold: f.fold, Kinds: f.kinds, Institution: f.institution, CardLast4: f.card, AccountID: f.account,
		FromDate: f.from, ToDate: f.to, ClosingFrom: f.closingFrom, ClosingTo: f.closingTo,
		Overseas: f.overseas, AccountKind: f.accountKind, HideTransfers: f.hideTransfers, Query: f.query,
	}
}

// byKind leaves out the kind filter: the kind facet's own.
func (f searchFilter) byKind() dbq.KindTotalsParams {
	return dbq.KindTotalsParams{
		Institution: f.institution, CardLast4: f.card, AccountID: f.account,
		FromDate: f.from, ToDate: f.to, ClosingFrom: f.closingFrom, ClosingTo: f.closingTo,
		Overseas: f.overseas, AccountKind: f.accountKind, HideTransfers: f.hideTransfers, Query: f.query,
	}
}

func (s *Server) SearchTransactions(ctx context.Context, request SearchTransactionsRequestObject) (SearchTransactionsResponseObject, error) {
	params := request.Params
	filter := readSearch(params)
	sort := string(Newest)
	if params.Sort != nil && params.Sort.Valid() {
		sort = string(*params.Sort)
	}
	limit, offset := clamp(params.Limit, defaultLimit, 0, 500), clamp(params.Offset, 0, 0, math.MaxInt32)

	queries := dbq.New(s.pool)
	result := TransactionSearch{Items: []Transaction{}}
	if limit > 0 {
		rows, err := queries.SearchTransactions(ctx, filter.search(sort, limit, offset))
		if err != nil {
			return nil, fmt.Errorf("search transactions: %w", err)
		}
		for _, row := range rows {
			tx, err := toTransaction(dbq.ListTransactionsRow(row))
			if err != nil {
				return nil, err
			}
			result.Items = append(result.Items, tx)
		}
	}
	groups, err := queries.TransactionTotals(ctx, filter.totals())
	if err != nil {
		return nil, fmt.Errorf("total transactions: %w", err)
	}
	kinds, err := queries.KindTotals(ctx, filter.byKind())
	if err != nil {
		return nil, fmt.Errorf("total transactions by kind: %w", err)
	}
	summary := summarize(groups)
	result.Total, result.Sum, result.Flows = summary.total.count, numeric.String(summary.total.amount), summary.flows
	result.ByKind, result.ByCard, result.ByAccount = kindTotals(kinds), summary.byCard, summary.byAccount
	return SearchTransactions200JSONResponse(result), nil
}
