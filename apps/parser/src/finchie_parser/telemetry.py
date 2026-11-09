"""Traces, metrics, and logs over OTLP when OTEL_EXPORTER_OTLP_ENDPOINT is set, to whatever collector the
deployment runs (ADR 0011). The standard OTEL_* variables configure it (OTEL_SERVICE_NAME,
OTEL_RESOURCE_ATTRIBUTES). Without an endpoint nothing leaves the process: the global providers stay
no-op and logs go to stdout only.
"""

import logging
import os

from opentelemetry import metrics, trace
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.logging.handler import LoggingHandler
from opentelemetry.instrumentation.system_metrics import SystemMetricsInstrumentor
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import OTELResourceDetector, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

# The process's own, not the host's, which the deployment watches by other means. Memory is the one to
# watch: OCR loads ONNX models and renders pages at 3x.
_PROCESS_METRICS: dict[str, list[str] | None] = {
    "process.cpu.time": ["user", "system"],
    "process.memory.usage": None,
    "process.thread.count": None,
    "cpython.gc.collections": None,
}


def enabled() -> bool:
    """Whether telemetry leaves the process."""
    return bool(os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"))


def setup(version: str) -> None:
    """Install the global tracer, meter, and logger providers, and send log records over OTLP too, with the
    trace they were logged in. Call it after logging is configured; stdout keeps its handler.

    The providers flush what is still buffered at exit: the SDK registers that itself.
    """
    if not enabled():
        return
    res = resource(version)

    # The exporters read OTEL_EXPORTER_OTLP_ENDPOINT; an http:// endpoint means plaintext.
    tracer_provider = TracerProvider(resource=res)
    tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    meter_provider = MeterProvider(resource=res, metric_readers=[PeriodicExportingMetricReader(OTLPMetricExporter())])
    logger_provider = LoggerProvider(resource=res)
    logger_provider.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter()))
    # The propagators default to W3C trace context and baggage, as the backend sends them.
    trace.set_tracer_provider(tracer_provider)
    metrics.set_meter_provider(meter_provider)
    set_logger_provider(logger_provider)

    SystemMetricsInstrumentor(config=_PROCESS_METRICS).instrument(meter_provider=meter_provider)
    logging.getLogger().addHandler(LoggingHandler(logger_provider=logger_provider))


def resource(version: str) -> Resource:
    """`finchie-parser` at this release, unless OTEL_SERVICE_NAME or OTEL_RESOURCE_ATTRIBUTES say otherwise."""
    # Resource.create lets its arguments win over the environment; merging the environment again on top
    # lets the deployment rename the service, as the backend allows.
    defaults = Resource.create({"service.name": "finchie-parser", "service.version": version})
    return defaults.merge(OTELResourceDetector().detect())
