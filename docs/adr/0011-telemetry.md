# ADR 0011: Telemetry

## Decision

The backend sends traces, metrics, and logs with OpenTelemetry over OTLP (gRPC), to whatever collector the deployment points it at. With mail ingestion (ADR 0010), statements arrive while nobody is looking; when one goes wrong, its traces show the whole path: the upload, and linked to it the parse job with its call to the parser.

| | |
|---|---|
| Switch | `OTEL_EXPORTER_OTLP_ENDPOINT`. Unset (local development, tests), the providers stay no-op and logs go to stdout only. The standard `OTEL_*` variables configure the rest: `OTEL_SERVICE_NAME` (default `finchie-backend`), `OTEL_RESOURCE_ATTRIBUTES` (such as `service.namespace` and `deployment.environment.name`) |
| Version | `service.version` on everything, set at build time (`-ldflags -X main.version`, the release tag in `release.yml`), so behaviour before and after a release can be told apart |
| Traces | Every request but `/healthz` (`otelhttp` around the server, named by the route Echo matched: `GET /api/statements/:id`); the parse job (River's `otelriver` middleware, `river.work/parse_document`, its span linked back to the upload that enqueued it through the job's metadata); the gRPC call to the parser (`otelgrpc`, which passes the trace context on) |
| Metrics | HTTP request duration and sizes by route, River job metrics, Go runtime, and `finchie.documents{status}`: documents by status, read from the database at each collection, for alerting on statements that failed to parse or do not reconcile (since ADR 0013 also `finchie.accounts.gaps`, accounts whose balances do not join up) |
| Logs | `slog` writes to stdout and, through the `otelslog` bridge, over OTLP with the trace a record was logged in (the same records: info and up. The bridge on its own took every level, so River's debug records went over OTLP until 2.7.1). A deployment that also collects the container's stdout should stop, or every line arrives twice; what is printed before telemetry starts (a startup failure) is then on stdout only |

The parser is not instrumented yet; the backend already propagates the trace context to it, so its spans will join the same traces once it is. (It is now, the same way: the same switch and `OTEL_*` variables, `finchie-parser` by default, `service.version` from the image (`PARSER_VERSION`). A span for every call but health checks, a child of the backend's call to it, with spans for picking the statement, parsing, and each OCR; process metrics (CPU, memory, threads, garbage collection); and `logging` records over OTLP through `opentelemetry-instrumentation-logging`, besides stdout.)

## Alternatives considered

- **A Prometheus `/metrics` endpoint to scrape.** No dependencies and enough for one alert, but metrics only: traces and logs would still need OTLP, and a deployment would collect two ways.
- **OTel with a Prometheus exporter (pull).** OpenTelemetry's API behind a scraped endpoint: the same two ways, for no gain.
