package telemetry

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HealthPath is left out of traces: Docker and the monitoring probe call it every few seconds.
const HealthPath = "/healthz"

// Handler wraps the whole server: a span and the http.server metrics for every request but the health
// check. It runs before Echo routes the request, so Route fills in the route.
func Handler(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "http.server", otelhttp.WithFilter(func(r *http.Request) bool {
		return r.URL.Path != HealthPath
	}))
}

// Route names the request's span after the route Echo matched ("GET /api/statements/:id") and adds
// it to the request's metrics, so neither is split by every ID.
func Route() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if route := c.Path(); route != "" {
				request := c.Request()
				span := trace.SpanFromContext(request.Context())
				span.SetName(request.Method + " " + route)
				span.SetAttributes(attribute.String("http.route", route))
				if labeler, ok := otelhttp.LabelerFromContext(request.Context()); ok {
					labeler.Add(attribute.String("http.route", route))
				}
			}
			return next(c)
		}
	}
}

// AccessLog logs each request but the health check: its method, route, status, latency, and sizes,
// in the request's trace. Never its query, which carries what the owner searched for (a merchant, an
// amount), nor the client's address or browser: logs leave the process (ADR 0011).
func AccessLog() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper:          func(c *echo.Context) bool { return c.Request().URL.Path == HealthPath },
		LogMethod:        true,
		LogRoutePath:     true,
		LogStatus:        true,
		LogLatency:       true,
		LogContentLength: true,
		LogResponseSize:  true,
		HandleError:      true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			level, message := slog.LevelInfo, "request"
			if v.Error != nil {
				level, message = slog.LevelError, "request failed"
			}
			attrs := []slog.Attr{
				slog.String("method", v.Method), slog.String("route", v.RoutePath), slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency), slog.String("bytes_in", v.ContentLength), slog.Int64("bytes_out", v.ResponseSize),
			}
			if v.Error != nil {
				attrs = append(attrs, slog.String("error", v.Error.Error()))
			}
			c.Logger().LogAttrs(c.Request().Context(), level, message, attrs...)
			return nil
		},
	})
}
