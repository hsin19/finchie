package ingest

import (
	"context"
	"fmt"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// storeHoldings adds a securities statement's trades' money, then works out again which securities
// each of its accounts held none of on each statement's day: a statement's holdings are the whole
// account, so what it does not list is at 0 (ADR 0017). Like links, derived from everything stored,
// since statements arrive in any order.
func storeHoldings(ctx context.Context, queries *dbq.Queries, statementID int64, trades dbq.InsertTradesParams, accountIDs []int64) error {
	if len(trades.LineNos) > 0 {
		trades.StatementID = statementID
		if err := queries.InsertTrades(ctx, trades); err != nil {
			return fmt.Errorf("store trades: %w", err)
		}
	}
	for _, id := range accountIDs {
		if err := queries.ClearEmptyHoldings(ctx, id); err != nil {
			return fmt.Errorf("clear empty holdings: %w", err)
		}
		if err := queries.ImplyEmptyHoldings(ctx, id); err != nil {
			return fmt.Errorf("imply empty holdings: %w", err)
		}
	}
	return nil
}
