// Package telemetry sends traces, metrics, and logs over OTLP when OTEL_EXPORTER_OTLP_ENDPOINT is set,
// to whatever collector the deployment runs (ADR 0011). The standard OTEL_* variables configure it
// (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES).
// Without an endpoint nothing leaves the process and logs go to stdout only.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Scope names the backend's own tracers, meters, and loggers.
const Scope = "github.com/hsin19/finchie/apps/backend"

// Enabled reports whether telemetry leaves the process.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// Setup installs the global tracer, meter, and logger providers and returns the logger to use: it
// writes to stdout and, when enabled, over OTLP too, with the trace a record was logged in. Shutdown
// flushes what is still buffered.
func Setup(ctx context.Context, version string, stdout slog.Handler) (*slog.Logger, func(context.Context) error, error) {
	if !Enabled() {
		return slog.New(stdout), func(context.Context) error { return nil }, nil
	}

	// Plain attributes rather than a semconv package: its schema URL would have to match the SDK's.
	// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES, read later, override the name.
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", "finchie-backend"), attribute.String("service.version", version)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry resource: %w", err)
	}

	// The exporters read OTEL_EXPORTER_OTLP_ENDPOINT; an http:// endpoint means plaintext.
	traces, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("trace exporter: %w", err)
	}
	metrics, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("metric exporter: %w", err)
	}
	logs, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("log exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traces), sdktrace.WithResource(res))
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics)), sdkmetric.WithResource(res))
	loggerProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logs)), sdklog.WithResource(res))
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetLoggerProvider(loggerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if err := runtime.Start(); err != nil {
		return nil, nil, fmt.Errorf("runtime metrics: %w", err)
	}

	shutdown := func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx), loggerProvider.Shutdown(ctx))
	}
	return newLogger(stdout, loggerProvider), shutdown, nil
}

// newLogger writes to stdout and over OTLP the same records: otelslog's handler takes every level,
// and libraries such as River log at debug.
func newLogger(stdout slog.Handler, provider *sdklog.LoggerProvider) *slog.Logger {
	otlp := otelslog.NewHandler(Scope, otelslog.WithLoggerProvider(provider))
	return slog.New(slog.NewMultiHandler(stdout, levelOf{otlp, stdout}))
}

// levelOf passes a record on only at the levels the reference handler takes.
type levelOf struct {
	slog.Handler
	reference slog.Handler
}

func (h levelOf) Enabled(ctx context.Context, level slog.Level) bool {
	return h.reference.Enabled(ctx, level) && h.Handler.Enabled(ctx, level)
}

func (h levelOf) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelOf{h.Handler.WithAttrs(attrs), h.reference}
}

func (h levelOf) WithGroup(name string) slog.Handler {
	return levelOf{h.Handler.WithGroup(name), h.reference}
}
