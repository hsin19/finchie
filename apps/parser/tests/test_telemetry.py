import logging
from collections.abc import Iterator

import grpc
import pytest
from fakes import FakeParser
from grpc_health.v1 import health_pb2, health_pb2_grpc
from opentelemetry import trace
from opentelemetry.instrumentation.logging.handler import LoggingHandler
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import InMemoryLogRecordExporter, SimpleLogRecordProcessor
from opentelemetry.sdk.trace import ReadableSpan, TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import SpanKind, StatusCode
from pdfs import email_with, text_pdf

from finchie.parser.v1 import parser_pb2 as pb
from finchie.parser.v1 import parser_pb2_grpc
from finchie_parser import server as server_module
from finchie_parser import telemetry
from finchie_parser.model import ParseResult
from finchie_parser.settings import Settings

# What the backend's otelgrpc client sends: the trace and the span of its call to the parser.
TRACE_ID = "4bf92f3577b34da6a3ce929d0e0e4736"
CALLER_SPAN_ID = "00f067aa0ba902b7"
TRACEPARENT = (("traceparent", f"00-{TRACE_ID}-{CALLER_SPAN_ID}-01"),)
# The instrumentation names a call's span after its full method.
PARSE_DOCUMENT = f"/{server_module.SERVICE_NAME}/ParseDocument"


@pytest.fixture(scope="module")
def exporter() -> InMemorySpanExporter:
    # The global provider can be set once per process; the parser's tracers are proxies that follow it.
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    trace.set_tracer_provider(provider)
    return exporter


@pytest.fixture
def spans(exporter: InMemorySpanExporter) -> Iterator[InMemorySpanExporter]:
    exporter.clear()
    yield exporter
    exporter.clear()


@pytest.fixture
def channel() -> Iterator[grpc.Channel]:
    settings = Settings(ocr=False, workers=2)
    server, port = server_module.build_server(settings, None, "127.0.0.1:0", parsers=[FakeParser()])
    server.start()
    with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
        yield channel
    server.stop(grace=None)


def parse(channel: grpc.Channel, content: bytes) -> pb.ParseDocumentResponse:
    stub = parser_pb2_grpc.ParserServiceStub(channel)
    request = pb.ParseDocumentRequest(
        document_id="d1", filename="mail.eml", content_type="message/rfc822", content=content
    )
    # SimpleSpanProcessor exports a span as it ends, which is before the server answers.
    response: pb.ParseDocumentResponse = stub.ParseDocument(request, metadata=TRACEPARENT, timeout=10)
    return response


def by_name(spans: InMemorySpanExporter) -> dict[str, ReadableSpan]:
    return {span.name: span for span in spans.get_finished_spans()}


def test_a_parse_joins_the_backends_trace_with_its_steps_and_result(
    channel: grpc.Channel, spans: InMemorySpanExporter
) -> None:
    parse(channel, email_with(("FAKE_1.pdf", "application/pdf", text_pdf("FAKE BANK credit card statement"))))

    named = by_name(spans)
    call = named[PARSE_DOCUMENT]
    assert call.kind is SpanKind.SERVER
    assert call.parent is not None
    assert (call.parent.trace_id, call.parent.span_id) == (int(TRACE_ID, 16), int(CALLER_SPAN_ID, 16))
    assert (
        dict(call.attributes or {}).items()
        >= {
            "finchie.document.id": "d1",
            "finchie.parser.name": "fake.statement",
            "finchie.parser.version": "7",
            "finchie.statement.lines": 0,
            "finchie.statement.balanced": True,
        }.items()
    )
    for step in ("select_statement", "parse"):
        assert named[step].parent == call.context
    assert (named["parse"].attributes or {})["finchie.parser.name"] == "fake.statement"


def test_health_checks_are_not_traced(channel: grpc.Channel, spans: InMemorySpanExporter) -> None:
    health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(), metadata=TRACEPARENT, timeout=5)

    assert spans.get_finished_spans() == ()


def test_a_rejected_document_is_an_error_with_its_status_code(
    channel: grpc.Channel, spans: InMemorySpanExporter
) -> None:
    with pytest.raises(grpc.RpcError):
        parse(channel, email_with())

    call = by_name(spans)[PARSE_DOCUMENT]
    assert call.status.status_code is StatusCode.ERROR
    assert (call.attributes or {})["rpc.grpc.status_code"] == grpc.StatusCode.UNIMPLEMENTED.value[0]


def test_a_bug_marks_the_call_failed_with_the_exception(
    channel: grpc.Channel, spans: InMemorySpanExporter, monkeypatch: pytest.MonkeyPatch
) -> None:
    def broken(*_: object, **__: object) -> ParseResult:
        raise RuntimeError("index out of range")

    monkeypatch.setattr(server_module, "parse_document", broken)

    with pytest.raises(grpc.RpcError):
        parse(channel, email_with())

    call = by_name(spans)[PARSE_DOCUMENT]
    assert call.status.status_code is StatusCode.ERROR
    assert call.status.description == "RuntimeError: index out of range"
    assert {(event.attributes or {}).get("exception.type") for event in call.events} == {"RuntimeError"}


def test_records_logged_during_a_call_carry_its_trace(channel: grpc.Channel, spans: InMemorySpanExporter) -> None:
    records = InMemoryLogRecordExporter()
    provider = LoggerProvider()
    provider.add_log_record_processor(SimpleLogRecordProcessor(records))
    handler = LoggingHandler(logger_provider=provider)
    logger = logging.getLogger("finchie_parser")
    logger.addHandler(handler)
    try:
        with pytest.raises(grpc.RpcError):
            parse(channel, email_with())
    finally:
        logger.removeHandler(handler)

    [rejected] = [r.log_record for r in records.get_finished_logs() if "rejected" in str(r.log_record.body)]
    assert rejected.trace_id == int(TRACE_ID, 16)


def test_the_resource_names_the_parser_at_its_release(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("OTEL_SERVICE_NAME", raising=False)
    monkeypatch.delenv("OTEL_RESOURCE_ATTRIBUTES", raising=False)

    attributes = telemetry.resource("2.4.0").attributes

    assert (attributes["service.name"], attributes["service.version"]) == ("finchie-parser", "2.4.0")


def test_the_deployment_names_the_service_and_adds_attributes(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OTEL_SERVICE_NAME", "parser-staging")
    monkeypatch.setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=finchie,deployment.environment.name=production")

    attributes = telemetry.resource("2.4.0").attributes

    assert attributes["service.name"] == "parser-staging"
    assert attributes["service.version"] == "2.4.0"
    assert (attributes["service.namespace"], attributes["deployment.environment.name"]) == ("finchie", "production")


def test_without_an_endpoint_nothing_is_installed(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("OTEL_EXPORTER_OTLP_ENDPOINT", raising=False)
    provider, handlers = trace.get_tracer_provider(), list(logging.getLogger().handlers)

    telemetry.setup("2.4.0")

    assert trace.get_tracer_provider() is provider
    assert logging.getLogger().handlers == handlers
