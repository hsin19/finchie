package telemetry

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestsAreTracedByRoute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	e := echo.New()
	e.Use(Route())
	ok := func(c *echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/api/statements/:id", ok)
	e.GET(HealthPath, ok)
	server := Handler(e)

	for _, path := range []string{"/api/statements/42", HealthPath} {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1 (the health check is not traced)", len(spans))
	}
	if name := spans[0].Name(); name != "GET /api/statements/:id" {
		t.Fatalf("span name = %q, want the route, not the path", name)
	}
}

func TestTheAccessLogNamesTheRouteNeverTheQueryOrTheClient(t *testing.T) {
	var logged bytes.Buffer
	e := echo.New()
	e.Logger = slog.New(slog.NewTextHandler(&logged, nil))
	e.Use(AccessLog())
	e.GET("/api/transactions", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET(HealthPath, func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/transactions?q=PHARMACY", nil)
	request.Header.Set("User-Agent", "a-browser")
	e.ServeHTTP(httptest.NewRecorder(), request)
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, HealthPath, nil))

	line := logged.String()
	if !strings.Contains(line, "route=/api/transactions") || !strings.Contains(line, "status=200") {
		t.Fatalf("log = %q, want the route and status", line)
	}
	for _, leak := range []string{"PHARMACY", "a-browser", "192.0.2", HealthPath} {
		if strings.Contains(line, leak) {
			t.Fatalf("log = %q, has %q", line, leak)
		}
	}
}
