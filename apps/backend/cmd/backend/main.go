package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/riverqueue/rivercontrib/otelriver"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/hsin19/finchie/apps/backend/internal/access"
	"github.com/hsin19/finchie/apps/backend/internal/api"
	"github.com/hsin19/finchie/apps/backend/internal/config"
	"github.com/hsin19/finchie/apps/backend/internal/db"
	parserv1 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v1"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	"github.com/hsin19/finchie/apps/backend/internal/telemetry"
	"github.com/hsin19/finchie/apps/backend/internal/web"
)

// version is the release, set at build time (-ldflags "-X main.version=2.4.0"); telemetry carries it.
var version = "dev"

// jobStopTimeout is how long a parse running at shutdown gets to finish; then its context is cancelled
// and River retries it after the restart.
const jobStopTimeout = 30 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := os.Getenv("BACKEND_HTTP_ADDR")
		if addr == "" {
			addr = ":8080"
		}
		if err := healthcheck(addr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger, shutdownTelemetry, err := telemetry.Setup(context.Background(), version, newStdoutHandler(cfg.LogFormat))
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flushCtx); err != nil {
			slog.Warn("flushing telemetry", "error", err)
		}
	}()
	if telemetry.Enabled() {
		slog.Info("sending traces, metrics, and logs over OTLP", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "version", version)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	linked, err := ingest.Relink(ctx, pool)
	if err != nil {
		return err
	}
	slog.Info("linked transactions to their parents", "children", linked)
	if err := telemetry.ObserveDocuments(pool, otel.Meter(telemetry.Scope)); err != nil {
		return err
	}
	if err := telemetry.ObserveAccountGaps(pool, otel.Meter(telemetry.Scope)); err != nil {
		return err
	}

	// The parser is internal and stateless (ADR 0002): plaintext on the private network. A request
	// carries a whole document; an opened one comes back no larger (ADR 0012).
	messageLimit := int(cfg.MaxUploadBytes()) + 1<<20
	limit := grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(messageLimit), grpc.MaxCallRecvMsgSize(messageLimit))
	conn, err := grpc.NewClient(cfg.ParserAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), limit,
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return fmt.Errorf("parser client: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("closing parser connection", "error", err)
		}
	}()

	workers := river.NewWorkers()
	parser := parserv1.NewParserServiceClient(conn)
	river.AddWorker(workers, ingest.NewWorker(pool, parser, cfg.ParseTimeout))
	jobs, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.ParseConcurrency}},
		Workers: workers,
		Logger:  logger,
		// Without it, the signal that cancels the context passed to Start cancels running jobs too.
		SoftStopTimeout: jobStopTimeout,
		// The parse job's span links back to the upload that enqueued it, and is named after the job.
		Middleware: []rivertype.Middleware{otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
			EnableTracePropagation:      true,
			EnableWorkSpanJobKindSuffix: true,
		})},
	})
	if err != nil {
		return fmt.Errorf("job client: %w", err)
	}
	if err := jobs.Start(ctx); err != nil {
		return fmt.Errorf("start jobs: %w", err)
	}

	e := echo.New()
	e.Logger = logger
	e.Use(telemetry.Route(), middleware.Recover(), middleware.RequestLogger(), middleware.BodyLimit(cfg.MaxUploadBytes()+1<<20))
	if cfg.AccessEnabled() {
		e.Use(access.New(ctx, cfg.AccessTeamDomain, cfg.AccessAudience).Middleware(telemetry.HealthPath))
		slog.Info("requests must come through Cloudflare Access", "team", cfg.AccessTeamDomain)
	} else {
		slog.Warn("Cloudflare Access verification is off: anyone who reaches the backend can use it " +
			"(set BACKEND_ACCESS_TEAM_DOMAIN and BACKEND_ACCESS_AUD in production)")
	}
	e.GET(telemetry.HealthPath, func(c *echo.Context) error {
		if err := pool.Ping(c.Request().Context()); err != nil {
			return c.String(http.StatusServiceUnavailable, "database unreachable")
		}
		return c.String(http.StatusOK, "ok")
	})
	api.Register(e, api.NewServer(pool, jobs, parser, cfg.MaxUploadBytes(), cfg.MailboxAddress))
	web.Register(e)

	slog.Info("listening", "addr", cfg.HTTPAddr, "parser", cfg.ParserAddr)
	serveErr := echo.StartConfig{Address: cfg.HTTPAddr, HideBanner: true, GracefulTimeout: 10 * time.Second}.Start(ctx, telemetry.Handler(e))

	// River's soft stop started with the signal; the extra seconds let the jobs it then cancels return
	// and their retries be recorded.
	stopCtx, cancel := context.WithTimeout(context.Background(), jobStopTimeout+5*time.Second)
	defer cancel()
	if err := jobs.Stop(stopCtx); err != nil {
		slog.Warn("jobs did not stop cleanly", "error", err)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", serveErr)
	}
	return nil
}

func newStdoutHandler(format string) slog.Handler {
	if format == "text" {
		return slog.NewTextHandler(os.Stdout, nil)
	}
	return slog.NewJSONHandler(os.Stdout, nil)
}
