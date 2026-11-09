import logging
import os
import signal
import time
from concurrent import futures
from types import FrameType

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from grpc_reflection.v1alpha import reflection
from opentelemetry import trace
from opentelemetry.instrumentation.grpc import filters, server_interceptor
from opentelemetry.trace import StatusCode

from finchie.parser.v1 import parser_pb2, parser_pb2_grpc
from finchie_parser import telemetry
from finchie_parser.convert import to_open_response, to_response
from finchie_parser.errors import (
    InvalidDocumentError,
    LayoutError,
    PasswordRequiredError,
    UnsupportedDocumentError,
)
from finchie_parser.model import DepositStatement, SecuritiesStatement
from finchie_parser.ocr import Ocr, RapidOcr
from finchie_parser.pipeline import parse_document
from finchie_parser.reading import open_document
from finchie_parser.settings import Settings

log = logging.getLogger("finchie_parser")

SERVICE_NAME = parser_pb2.DESCRIPTOR.services_by_name["ParserService"].full_name

# ADR 0005: the status code tells the backend whether a retry can help.
_STATUS = (
    (UnsupportedDocumentError, grpc.StatusCode.UNIMPLEMENTED),
    (InvalidDocumentError, grpc.StatusCode.INVALID_ARGUMENT),
    (PasswordRequiredError, grpc.StatusCode.FAILED_PRECONDITION),
    (LayoutError, grpc.StatusCode.INTERNAL),
)


class ParserService(parser_pb2_grpc.ParserServiceServicer):
    def __init__(self, settings: Settings, ocr: Ocr | None) -> None:
        self._passwords = settings.passwords()
        self._ocr = ocr

    def ParseDocument(  # noqa: N802 - name fixed by the generated base class
        self, request: parser_pb2.ParseDocumentRequest, context: grpc.ServicerContext
    ) -> parser_pb2.ParseDocumentResponse:
        started = time.monotonic()
        span = trace.get_current_span()
        span.set_attribute("finchie.document.id", request.document_id)
        try:
            result = parse_document(request.content, passwords=self._passwords, ocr=self._ocr)
        except Exception as error:
            _reject(context, request.document_id, error)
            raise
        statement = result.statement
        if isinstance(statement, DepositStatement):
            lines, balanced = sum(len(a.lines) for a in statement.accounts), statement.balanced
        elif isinstance(statement, SecuritiesStatement):
            lines, balanced = len(statement.trades), statement.balanced
        else:
            lines, balanced = len(statement.transactions), statement.reconciliation.balanced
        span.set_attributes(
            {
                "finchie.parser.name": result.parser,
                "finchie.parser.version": result.parser_version,
                "finchie.statement.lines": lines,
                "finchie.statement.balanced": balanced,
            }
        )
        log.info(
            "document %s parsed by %s v%s: %d lines, balanced=%s, %.1fs",
            request.document_id,
            result.parser,
            result.parser_version,
            lines,
            balanced,
            time.monotonic() - started,
        )
        return to_response(result)

    def OpenDocument(  # noqa: N802 - name fixed by the generated base class
        self, request: parser_pb2.OpenDocumentRequest, context: grpc.ServicerContext
    ) -> parser_pb2.OpenDocumentResponse:
        trace.get_current_span().set_attribute("finchie.document.id", request.document_id)
        try:
            opened = open_document(request.content, passwords=self._passwords)
        except Exception as error:
            _reject(context, request.document_id, error)
            raise
        return to_open_response(opened)


def _reject(context: grpc.ServicerContext, document_id: str, error: Exception) -> None:
    """Abort with the status code of a known error; anything else is logged as a bug."""
    for error_type, status in _STATUS:
        if isinstance(error, error_type):
            log.warning("document %s rejected (%s): %s", document_id, status.name, error)
            context.abort(status, str(error))
    log.exception("document %s failed", document_id)
    # The interceptor records the exception on the span but leaves its status to an abort, which a bug skips.
    trace.get_current_span().set_status(StatusCode.ERROR, f"{type(error).__name__}: {error}")


def build_server(settings: Settings, ocr: Ocr | None, address: str | None = None) -> tuple[grpc.Server, int]:
    limit = settings.max_message_mb * 1024 * 1024
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=settings.workers),
        # A span for every call but health checks, which the container makes every 30 seconds. It joins the
        # backend's trace through the trace context in the call's metadata. No-op until telemetry.setup.
        interceptors=[server_interceptor(filter_=filters.negate(filters.health_check()))],
        options=[("grpc.max_receive_message_length", limit), ("grpc.max_send_message_length", limit)],
    )
    parser_pb2_grpc.add_ParserServiceServicer_to_server(ParserService(settings, ocr), server)

    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    for name in ("", SERVICE_NAME):
        health_servicer.set(name, health_pb2.HealthCheckResponse.SERVING)

    reflection.enable_server_reflection((SERVICE_NAME, health.SERVICE_NAME, reflection.SERVICE_NAME), server)
    port = server.add_insecure_port(address or f"{settings.host}:{settings.port}")
    return server, port


def main() -> None:
    settings = Settings()
    logging.basicConfig(level=settings.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    logging.getLogger("RapidOCR").setLevel(logging.WARNING)
    telemetry.setup(settings.version)
    if telemetry.enabled():
        log.info(
            "sending traces, metrics, and logs over OTLP to %s (version %s)",
            os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"],
            settings.version,
        )

    ocr = RapidOcr() if settings.ocr else None
    server, port = build_server(settings, ocr)
    server.start()
    log.info(
        "listening on %s:%d (workers=%d, ocr=%s, %r)",
        settings.host,
        port,
        settings.workers,
        settings.ocr,
        settings.passwords(),
    )

    def stop(signum: int, _frame: FrameType | None) -> None:
        log.info("received %s, draining", signal.Signals(signum).name)
        server.stop(grace=10)

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    server.wait_for_termination()
