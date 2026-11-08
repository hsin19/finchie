package ingest

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// Deposit statements of an account can share days when they are exports the owner picks the dates of,
// such as Cathay's e-passbook (ADR 0013). Over the shared days only one statement's lines and balances
// stand, so nothing is counted twice and the owner never has to pick dates that do not overlap.

// span is a deposit statement's place among those it may overlap.
type span struct {
	id, documentID int64
	start, end     time.Time
}

// keeps reports whether a's lines and balances stand over the days it shares with b: the statement that
// closes later, since an export's last day may have been taken before that day ended; of two closing the
// same day, the later document. Neither depends on the order the statements are stored in, so storing
// or re-parsing them in any order gives the same lines.
func keeps(a, b span) bool {
	return cmp.Or(a.end.Compare(b.end), cmp.Compare(a.documentID, b.documentID)) > 0
}

// trimOverlaps works out, for the account in the unit, every pair the stored statement makes with
// another that shares days with it, and removes the lines and balances of whichever does not keep them.
func trimOverlaps(ctx context.Context, queries *dbq.Queries, this span, accountID int64, unit string) error {
	others, err := queries.OverlappingDepositStatements(ctx, dbq.OverlappingDepositStatementsParams{
		StatementID: this.id, AccountID: accountID, Unit: unit,
	})
	if err != nil {
		return fmt.Errorf("overlapping statements: %w", err)
	}
	for _, o := range others {
		keeper, other := this, span{id: o.ID, documentID: o.DocumentID, start: o.PeriodStart.Time, end: o.ClosingDate.Time}
		if keeps(other, this) {
			keeper, other = other, this
		}
		from, to := pgtype.Date{Time: keeper.start, Valid: true}, pgtype.Date{Time: keeper.end, Valid: true}
		if err := queries.TrimStatementLines(ctx, dbq.TrimStatementLinesParams{
			StatementID: other.id, AccountID: accountID, Unit: unit, FromDate: from, ToDate: to,
		}); err != nil {
			return fmt.Errorf("trim lines: %w", err)
		}
		if err := queries.TrimStatementBalances(ctx, dbq.TrimStatementBalancesParams{
			StatementID: other.id, AccountID: accountID, Unit: unit, FromDate: from, ToDate: to, KeeperID: keeper.id,
		}); err != nil {
			return fmt.Errorf("trim balances: %w", err)
		}
	}
	return nil
}
