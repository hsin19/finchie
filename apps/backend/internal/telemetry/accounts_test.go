package telemetry

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func TestAccountGapsCountsAccountsWhoseBalancesDoNotJoin(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := t.Context()
	queries := dbq.New(pool)
	// Two balances of one deposit account, with no line between them to explain the difference.
	doc, err := queries.UpsertDocument(ctx, dbq.UpsertDocumentParams{Sha256: []byte("d"), Filename: "d.eml", ContentType: "message/rfc822", Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) pgtype.Date {
		d, _ := time.Parse(time.DateOnly, s)
		return pgtype.Date{Time: d, Valid: true}
	}
	statement, err := queries.InsertStatement(ctx, dbq.InsertStatementParams{
		DocumentID: doc.ID, Institution: "esun", Kind: "deposit", PeriodStart: day("2026-09-01"), ClosingDate: day("2026-09-30"), Balanced: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := queries.UpsertAccount(ctx, dbq.UpsertAccountParams{Institution: "esun", Kind: "deposit", Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	for asOf, amount := range map[string]string{"2026-08-31": "100", "2026-09-30": "250"} {
		var n pgtype.Numeric
		if err := n.Scan(amount); err != nil {
			t.Fatal(err)
		}
		if err := queries.InsertBalance(ctx, dbq.InsertBalanceParams{AccountID: account, Unit: "TWD", AsOf: day(asOf), Amount: n, StatementID: statement}); err != nil {
			t.Fatal(err)
		}
	}

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	if err := ObserveAccountGaps(pool, provider.Meter(Scope)); err != nil {
		t.Fatal(err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "finchie.accounts.gaps" {
				if got := m.Data.(metricdata.Gauge[int64]).DataPoints[0].Value; got != 1 {
					t.Fatalf("finchie.accounts.gaps = %d, want 1", got)
				}
				return
			}
		}
	}
	t.Fatal("finchie.accounts.gaps not reported")
}
