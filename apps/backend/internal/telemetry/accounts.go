package telemetry

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// ObserveAccountGaps reports finchie.accounts.gaps: how many accounts have balances that do not join
// up, money that moved between two reports with no line to show for it, usually a statement that never
// arrived (ADR 0013). Worked out from the database at each collection; a deployment can alert while it
// is above zero.
func ObserveAccountGaps(pool *pgxpool.Pool, meter metric.Meter) error {
	gauge, err := meter.Int64ObservableGauge("finchie.accounts.gaps",
		metric.WithDescription("Accounts whose balances do not join up"),
		metric.WithUnit("{account}"))
	if err != nil {
		return fmt.Errorf("account gaps gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		gaps, err := dbq.New(pool).AccountGaps(ctx, nil)
		if err != nil {
			return fmt.Errorf("account gaps: %w", err)
		}
		accounts := map[int64]bool{}
		for _, gap := range gaps {
			// A move in a unit no statement lists a line of is one nothing could show (ADR 0013).
			if !gap.Itemized {
				continue
			}
			accounts[gap.AccountID] = true
		}
		observer.ObserveInt64(gauge, int64(len(accounts)))
		return nil
	}, gauge)
	if err != nil {
		return fmt.Errorf("account gaps gauge callback: %w", err)
	}
	return nil
}
