package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/testdb"
)

func TestDocumentsByStatus(t *testing.T) {
	pool := testdb.Pool(t)
	queries := dbq.New(pool)
	add := func(name string, status domain.DocumentStatus, failure domain.Failure) {
		t.Helper()
		doc, err := queries.UpsertDocument(t.Context(), dbq.UpsertDocumentParams{
			Sha256: []byte(name), Filename: name, ContentType: "application/pdf", Content: []byte("%PDF"),
		})
		if err != nil {
			t.Fatal(err)
		}
		finish := dbq.FinishDocumentParams{ID: doc.ID, Status: status, Warnings: []string{}}
		if failure != "" {
			finish.Failure = &failure
		}
		if err := queries.FinishDocument(t.Context(), finish); err != nil {
			t.Fatal(err)
		}
	}
	add("a.pdf", domain.Parsed, "")
	add("b.pdf", domain.Parsed, "")
	add("c.pdf", domain.NeedsReview, "")
	add("d.pdf", domain.Failed, domain.Layout)
	add("e.pdf", domain.Skipped, domain.Unsupported)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	if err := ObserveDocuments(pool, provider.Meter(Scope)); err != nil {
		t.Fatal(err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatal(err)
	}

	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "finchie.documents" {
				continue
			}
			for _, point := range m.Data.(metricdata.Gauge[int64]).DataPoints {
				status, _ := point.Attributes.Value(attribute.Key("status"))
				got[status.AsString()] = point.Value
			}
		}
	}
	want := map[string]int64{"pending": 0, "parsed": 2, "needs_review": 1, "duplicate": 0, "failed": 1, "skipped": 1}
	if len(got) != len(want) {
		t.Fatalf("finchie.documents = %v, want %v", got, want)
	}
	for status, n := range want {
		if got[status] != n {
			t.Errorf("finchie.documents{status=%q} = %d, want %d", status, got[status], n)
		}
	}
}
