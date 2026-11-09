package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// recordedBodies collects what reaches the OTLP pipeline.
type recordedBodies struct{ bodies []string }

func (r *recordedBodies) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (r *recordedBodies) OnEmit(_ context.Context, record *sdklog.Record) error {
	r.bodies = append(r.bodies, record.Body().AsString())
	return nil
}

func (r *recordedBodies) Shutdown(context.Context) error   { return nil }
func (r *recordedBodies) ForceFlush(context.Context) error { return nil }

func TestDebugRecordsStayOffOTLPAsOffStdout(t *testing.T) {
	recorded := &recordedBodies{}
	var stdout bytes.Buffer
	logger := newLogger(slog.NewTextHandler(&stdout, nil), sdklog.NewLoggerProvider(sdklog.WithProcessor(recorded)))

	logger.Debug("polling for jobs")
	logger.Info("listening")
	// River logs through a logger with its own attributes and groups.
	river := logger.With("component", "river").WithGroup("producer")
	river.Debug("fetched no jobs")
	river.Warn("job failed")

	if want := []string{"listening", "job failed"}; !slices.Equal(recorded.bodies, want) {
		t.Fatalf("over OTLP = %q, want %q", recorded.bodies, want)
	}
	if printed := stdout.String(); strings.Contains(printed, "jobs") || !strings.Contains(printed, "job failed") {
		t.Fatalf("stdout = %q, want the same records", printed)
	}
}
