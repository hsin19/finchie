import logging
import os
import signal
import time
import uuid
from collections.abc import Sequence
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
from finchie_parser.auth import TokenInterceptor
from finchie_parser.convert import to_open_response, to_response
from finchie_parser.errors import ParserError
from finchie_parser.ocr import Ocr, RapidOcr
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.parsers.base import institutions
from finchie_parser.pipeline import parse_document
from finchie_parser.reading import open_document
from finchie_parser.settings import Settings, variable

log = logging.getLogger("finchie_parser")

SERVICE_NAME = parser_pb2.DESCRIPTOR.services_by_name["ParserService"].full_name


class ParserService(parser_pb2_grpc.ParserServiceServicer):
    def __init__(self, settings: Settings, ocr: Ocr | None, parsers: Sequence[StatementParser] = PARSERS) -> None:
        self._passwords = settings.passwords(institutions(parsers))
        self._ocr = ocr
        self._parsers = parsers
        # New at every start: the configuration above is read once, at start (DescribeParsers).
        self._instance = uuid.uuid4().hex

    def ParseDocument(  # noqa: N802 - name fixed by the generated base class
        self, request: parser_pb2.ParseDocumentRequest, context: grpc.ServicerContext
    ) -> parser_pb2.ParseDocumentResponse:
        started = time.monotonic()
        span = trace.get_current_span()
        span.set_attribute("finchie.document.id", request.document_id)
        try:
            result = parse_document(
                request.content,
                passwords=self._passwords,
                ocr=self._ocr,
                parsers=self._parsers,
                # Past the backend's deadline, or once it cancels, nobody reads the answer.
                wanted=context.is_active,
            )
        except Exception as error:
            _reject(context, request.document_id, error)
            raise
        lines, balanced = result.statement.line_count, result.statement.balanced
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
            opened = open_document(request.content, passwords=self._passwords, parsers=self._parsers)
        except Exception as error:
            _reject(context, request.document_id, error)
            raise
        return to_open_response(opened)

    def DescribeParsers(  # noqa: N802 - name fixed by the generated base class
        self, request: parser_pb2.DescribeParsersRequest, context: grpc.ServicerContext
    ) -> parser_pb2.DescribeParsersResponse:
        return parser_pb2.DescribeParsersResponse(
            parsers=[parser_pb2.ParserVersion(name=p.name, version=p.version) for p in self._parsers],
            instance=self._instance,
        )


def _reject(context: grpc.ServicerContext, document_id: str, error: Exception) -> None:
    """Abort with the status code of a known error (ADR 0005, `errors.py`); anything else is logged as a bug."""
    if isinstance(error, ParserError):
        # The words go to the caller, which keeps them with the document; a parser's words can quote the
        # statement (an account number, a balance), and logs leave the process (ADR 0011).
        log.warning("document %s rejected (%s, %s)", document_id, error.status.name, type(error).__name__)
        context.abort(error.status, str(error))
    log.exception("document %s failed", document_id)
    # The interceptor records the exception on the span but leaves its status to an abort, which a bug skips.
    trace.get_current_span().set_status(StatusCode.ERROR, f"{type(error).__name__}: {error}")


def build_server(
    settings: Settings,
    ocr: Ocr | None,
    address: str | None = None,
    parsers: Sequence[StatementParser] = PARSERS,
) -> tuple[grpc.Server, int]:
    limit = settings.max_message_mb * 1024 * 1024
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=settings.workers),
        # A span for every call but health checks, which the container makes every 30 seconds. It joins the
        # backend's trace through the trace context in the call's metadata. No-op until telemetry.setup.
        interceptors=[
            server_interceptor(filter_=filters.negate(filters.health_check())),
            *([TokenInterceptor(settings.token.get_secret_value())] if settings.token else []),
        ],
        options=[("grpc.max_receive_message_length", limit), ("grpc.max_send_message_length", limit)],
    )
    parser_pb2_grpc.add_ParserServiceServicer_to_server(ParserService(settings, ocr, parsers), server)

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
    if ocr is None and (needing := [parser.name for parser in PARSERS if parser.needs_ocr]):
        log.warning("%s=false: %s cannot read their statements", variable("ocr"), ", ".join(needing))
    server, port = build_server(settings, ocr)
    server.start()
    log.info(
        "listening on %s:%d (workers=%d, ocr=%s, %r)",
        settings.host,
        port,
        settings.workers,
        settings.ocr,
        settings.passwords(institutions(PARSERS)),
    )

    def stop(signum: int, _frame: FrameType | None) -> None:
        log.info("received %s, draining", signal.Signals(signum).name)
        server.stop(grace=10)

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    server.wait_for_termination()
