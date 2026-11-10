"""The gRPC service: status codes, wiring, and capacity. Conversion is in test_convert.py."""

import threading
from collections.abc import Iterator
from datetime import UTC, datetime

import grpc
import pytest
from fakes import FakeParser
from grpc_health.v1 import health_pb2, health_pb2_grpc
from pdfs import PNG, blank_pdf, email_with, statement_email, text_pdf
from pydantic import SecretStr

from finchie.parser.v2 import parser_pb2 as pb
from finchie.parser.v2 import parser_pb2_grpc
from finchie_parser import server as server_module
from finchie_parser.errors import UnsupportedDocumentError
from finchie_parser.model import ParseResult
from finchie_parser.parsers import StatementParser
from finchie_parser.settings import Settings


def serve(parsers: list[StatementParser], settings: Settings | None = None) -> tuple[grpc.Server, int]:
    settings = settings or Settings(ocr=False, workers=1)
    server, port = server_module.build_server(settings, None, "127.0.0.1:0", parsers=parsers)
    server.start()
    return server, port


@pytest.fixture
def channel() -> Iterator[grpc.Channel]:
    server, port = serve([FakeParser()])
    with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
        yield channel
    server.stop(grace=None)


def stub(channel: grpc.Channel) -> parser_pb2_grpc.ParserServiceStub:
    return parser_pb2_grpc.ParserServiceStub(channel)


def parse(channel: grpc.Channel, content: bytes) -> pb.ParseDocumentResponse:
    response: pb.ParseDocumentResponse = stub(channel).ParseDocument(
        pb.ParseDocumentRequest(document_id="d1", content=content), timeout=10
    )
    return response


def status_of(channel: grpc.Channel, content: bytes) -> grpc.StatusCode:
    with pytest.raises(grpc.RpcError) as error:
        parse(channel, content)
    code: grpc.StatusCode = error.value.code()
    return code


def test_empty_document_is_invalid_argument(channel: grpc.Channel) -> None:
    assert status_of(channel, b"") is grpc.StatusCode.INVALID_ARGUMENT


def test_anything_but_an_email_is_invalid_argument(channel: grpc.Channel) -> None:
    assert status_of(channel, PNG) is grpc.StatusCode.INVALID_ARGUMENT
    assert status_of(channel, blank_pdf()) is grpc.StatusCode.INVALID_ARGUMENT


def test_an_email_no_parser_claims_is_unimplemented(channel: grpc.Channel) -> None:
    assert status_of(channel, email_with(sender="statements@elsewhere.example")) is grpc.StatusCode.UNIMPLEMENTED


def test_a_document_at_the_backend_upload_limit_reaches_the_parser_instead_of_exhausting_grpc(
    channel: grpc.Channel,
) -> None:
    # BACKEND_MAX_UPLOAD_MB is 32: the file alone fills it, and the request carries more fields. That the
    # parser refuses it as not an e-mail shows gRPC let it through (not RESOURCE_EXHAUSTED).
    content = PNG + bytes(32 * 1024 * 1024 - len(PNG))
    assert status_of(channel, content) is grpc.StatusCode.INVALID_ARGUMENT


def test_a_statement_is_parsed_by_the_parser_that_claims_it(channel: grpc.Channel) -> None:
    content = email_with(("FAKE_1.pdf", "application/pdf", text_pdf("FAKE BANK statement", user_password="secret")))
    response = parse(channel, content)
    assert (response.parser, response.parser_version, list(response.warnings)) == ("fake.statement", "7", ["a warning"])
    assert response.HasField("statement")


def test_a_chosen_password_for_an_unknown_institution_stops_the_server_from_starting() -> None:
    settings = Settings(pdf_passwords={"other": "abc"})
    with pytest.raises(ValueError, match="PARSER_PDF_PASSWORDS__OTHER"):
        server_module.build_server(settings, None, "127.0.0.1:0", parsers=[FakeParser()])


def test_only_the_v2_contract_is_served(channel: grpc.Channel) -> None:
    assert server_module.SERVICE_NAME == "finchie.parser.v2.ParserService"
    retired = channel.unary_unary("/finchie.parser.v1.ParserService/DescribeParsers")
    with pytest.raises(grpc.RpcError) as error:
        retired(b"", timeout=5)
    assert error.value.code() is grpc.StatusCode.UNIMPLEMENTED


def test_health_reports_serving(channel: grpc.Channel) -> None:
    health = health_pb2_grpc.HealthStub(channel)
    response = health.Check(health_pb2.HealthCheckRequest(service=server_module.SERVICE_NAME), timeout=5)
    assert response.status == health_pb2.HealthCheckResponse.SERVING


def test_health_and_previews_answer_while_the_backend_parses_two_documents(monkeypatch: pytest.MonkeyPatch) -> None:
    # BACKEND_PARSE_CONCURRENCY is 2; the container healthcheck gives up after 3 seconds.
    started, release = threading.Semaphore(0), threading.Event()

    def slow_parse(*_: object, **__: object) -> ParseResult:
        started.release()
        release.wait(10)
        raise UnsupportedDocumentError("released")

    monkeypatch.setattr(server_module, "parse_document", slow_parse)
    server, port = serve([FakeParser()], Settings(ocr=False))
    try:
        with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
            request = pb.ParseDocumentRequest(document_id="d1")
            parses = [stub(channel).ParseDocument.future(request, timeout=10) for _ in range(2)]
            assert all(started.acquire(timeout=5) for _ in parses)

            health = health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(), timeout=3)
            content = email_with(("x.pdf", "application/pdf", blank_pdf()))
            preview = stub(channel).OpenDocument(pb.OpenDocumentRequest(document_id="d2", content=content), timeout=3)
    finally:
        release.set()
        server.stop(grace=None)
    assert health.status == health_pb2.HealthCheckResponse.SERVING
    assert [f.filename for f in preview.files] == ["x.pdf"]


def test_open_document_returns_the_email_and_its_attachments_decrypted_by_the_claiming_parsers_rule(
    channel: grpc.Channel,
) -> None:
    request = pb.OpenDocumentRequest(document_id="d1", content=statement_email(blank_pdf("secret")))
    response = stub(channel).OpenDocument(request, timeout=10)
    assert response.email.subject == "範例銀行 2026年11月 信用卡電子帳單"
    assert response.email.date.ToDatetime(tzinfo=UTC) == datetime(2026, 11, 6, 1, 30, tzinfo=UTC)
    assert "data:image/png;base64," in response.email.html
    assert [(f.filename, f.content_type, f.locked, f.primary) for f in response.files] == [
        ("帳單_11511.pdf", "application/pdf", False, True),
        ("slip.html", "text/html", False, False),
    ]


def test_open_document_rejects_an_empty_document(channel: grpc.Channel) -> None:
    with pytest.raises(grpc.RpcError) as error:
        stub(channel).OpenDocument(pb.OpenDocumentRequest(document_id="d1"), timeout=10)
    assert error.value.code() is grpc.StatusCode.INVALID_ARGUMENT


def test_describe_parsers_names_each_parsers_version_and_a_new_instance_at_every_start() -> None:
    def describe() -> pb.DescribeParsersResponse:
        server, port = serve([FakeParser()])
        try:
            with grpc.insecure_channel(f"127.0.0.1:{port}") as ch:
                response: pb.DescribeParsersResponse = stub(ch).DescribeParsers(pb.DescribeParsersRequest())
                return response
        finally:
            server.stop(None)

    first, second = describe(), describe()
    assert [(p.name, p.version) for p in first.parsers] == [(FakeParser.name, FakeParser.version)]
    assert first.instance
    assert second.instance != first.instance


def test_with_a_token_only_a_caller_that_sends_it_is_served_but_health_checks_need_none() -> None:
    server, port = serve([FakeParser()], Settings(token=SecretStr("s3cret")))
    try:
        with grpc.insecure_channel(f"127.0.0.1:{port}") as ch:
            with pytest.raises(grpc.RpcError) as refused:
                stub(ch).DescribeParsers(pb.DescribeParsersRequest())
            assert refused.value.code() == grpc.StatusCode.UNAUTHENTICATED
            with pytest.raises(grpc.RpcError) as wrong:
                stub(ch).DescribeParsers(pb.DescribeParsersRequest(), metadata=[("authorization", "Bearer guess")])
            assert wrong.value.code() == grpc.StatusCode.UNAUTHENTICATED
            served: pb.DescribeParsersResponse = stub(ch).DescribeParsers(
                pb.DescribeParsersRequest(), metadata=[("authorization", "Bearer s3cret")]
            )
            assert served.parsers
            health = health_pb2_grpc.HealthStub(ch).Check(health_pb2.HealthCheckRequest())
            assert health.status == health_pb2.HealthCheckResponse.SERVING
    finally:
        server.stop(None)
