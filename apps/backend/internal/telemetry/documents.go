package telemetry

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
)

// ObserveDocuments reports finchie.documents{status}: uploaded documents by status, read from the database
// at each collection. Every status is reported, zero included, so an alert sees 0 rather than no series.
// A deployment can alert while any is failed or needs_review: a statement the mailbox brought in
// that nobody has looked at (ADR 0010).
func ObserveDocuments(pool *pgxpool.Pool, meter metric.Meter) error {
	gauge, err := meter.Int64ObservableGauge("finchie.documents",
		metric.WithDescription("Uploaded documents by status"),
		metric.WithUnit("{document}"))
	if err != nil {
		return fmt.Errorf("documents gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		rows, err := dbq.New(pool).DocumentCounts(ctx, nil)
		if err != nil {
			return fmt.Errorf("count documents: %w", err)
		}
		counts := map[domain.DocumentStatus]int64{}
		for _, row := range rows {
			counts[row.Status] = int64(row.Documents)
		}
		for _, status := range domain.DocumentStatuses {
			observer.ObserveInt64(gauge, counts[status], metric.WithAttributes(attribute.String("status", string(status))))
		}
		return nil
	}, gauge)
	if err != nil {
		return fmt.Errorf("documents gauge callback: %w", err)
	}
	return nil
}
