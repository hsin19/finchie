import threading
from collections.abc import Iterator
from datetime import UTC, date, datetime
from decimal import Decimal

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc
from pdfs import PNG, blank_pdf, email_with, statement_email

from finchie.parser.v1 import parser_pb2 as pb
from finchie.parser.v1 import parser_pb2_grpc
from finchie_parser import server as server_module
from finchie_parser.errors import UnsupportedDocumentError
from finchie_parser.model import (
    Kind,
    ParseResult,
    PointsExpiry,
    PointsKind,
    PointsLine,
    PointsProgram,
    Reconciliation,
    Statement,
    Transaction,
)
from finchie_parser.settings import Settings


@pytest.fixture
def channel() -> Iterator[grpc.Channel]:
    server, port = server_module.build_server(Settings(ocr=False, workers=1), None, "127.0.0.1:0")
    server.start()
    with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
        yield channel
    server.stop(grace=None)


def parse(channel: grpc.Channel, content: bytes, content_type: str) -> pb.ParseDocumentResponse:
    stub = parser_pb2_grpc.ParserServiceStub(channel)
    request = pb.ParseDocumentRequest(document_id="d1", filename="doc", content_type=content_type, content=content)
    return stub.ParseDocument(request, timeout=10)


def status_of(channel: grpc.Channel, content: bytes, content_type: str) -> grpc.StatusCode:
    with pytest.raises(grpc.RpcError) as error:
        parse(channel, content, content_type)
    return error.value.code()


def test_empty_document_is_invalid_argument(channel: grpc.Channel) -> None:
    assert status_of(channel, b"", "message/rfc822") is grpc.StatusCode.INVALID_ARGUMENT


def test_anything_but_an_email_is_invalid_argument(channel: grpc.Channel) -> None:
    assert status_of(channel, PNG, "image/png") is grpc.StatusCode.INVALID_ARGUMENT
    assert status_of(channel, blank_pdf(), "application/pdf") is grpc.StatusCode.INVALID_ARGUMENT


def test_an_email_no_parser_claims_is_unimplemented(channel: grpc.Channel) -> None:
    assert status_of(channel, email_with(), "message/rfc822") is grpc.StatusCode.UNIMPLEMENTED


def test_accepts_a_document_at_the_backend_upload_limit(channel: grpc.Channel) -> None:
    # BACKEND_MAX_UPLOAD_MB is 32: the file alone fills it, and the request carries more fields.
    content = PNG + bytes(32 * 1024 * 1024 - len(PNG))
    assert status_of(channel, content, "image/png") is grpc.StatusCode.INVALID_ARGUMENT


def test_health_reports_serving(channel: grpc.Channel) -> None:
    stub = health_pb2_grpc.HealthStub(channel)
    response = stub.Check(health_pb2.HealthCheckRequest(service=server_module.SERVICE_NAME), timeout=5)
    assert response.status == health_pb2.HealthCheckResponse.SERVING


def test_health_and_previews_answer_while_the_backend_parses_two_documents(monkeypatch: pytest.MonkeyPatch) -> None:
    # BACKEND_PARSE_CONCURRENCY is 2; the container healthcheck gives up after 3 seconds.
    started, release = threading.Semaphore(0), threading.Event()

    def slow_parse(*_: object, **__: object) -> ParseResult:
        started.release()
        release.wait(10)
        raise UnsupportedDocumentError("released")

    monkeypatch.setattr(server_module, "parse_document", slow_parse)
    server, port = server_module.build_server(Settings(_env_file=None, ocr=False), None, "127.0.0.1:0")
    server.start()
    try:
        with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
            stub = parser_pb2_grpc.ParserServiceStub(channel)
            request = pb.ParseDocumentRequest(document_id="d1", filename="doc", content_type="message/rfc822")
            parses = [stub.ParseDocument.future(request, timeout=10) for _ in range(2)]
            assert all(started.acquire(timeout=5) for _ in parses)

            health = health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(), timeout=3)
            content = email_with(("x.pdf", "application/pdf", blank_pdf()))
            preview = stub.OpenDocument(
                pb.OpenDocumentRequest(
                    document_id="d2", filename="x.eml", content_type="message/rfc822", content=content
                ),
                timeout=3,
            )
    finally:
        release.set()
        server.stop(grace=None)
    assert health.status == health_pb2.HealthCheckResponse.SERVING
    assert [f.filename for f in preview.files] == ["x.pdf"]


def test_result_is_converted_to_proto(channel: grpc.Channel, monkeypatch: pytest.MonkeyPatch) -> None:
    statement = Statement(
        institution="hsbc",
        closing_date=date(2026, 10, 21),
        due_date=date(2026, 11, 8),
        currency="TWD",
        total_due=Decimal("95730"),
        transactions=[
            Transaction(
                transaction_date=date(2026, 10, 8),
                posting_date=date(2026, 10, 9),
                description="SHOP",
                kind=Kind.PURCHASE,
                amount=Decimal(28009),
                original_amount=Decimal("780.00"),
                original_currency="EUR",
                country="IT",
            ),
            Transaction(date(2026, 9, 23), "PAYMENT", Kind.PAYMENT, Decimal(-69019)),
        ],
        reconciliation=Reconciliation(Decimal(28009), Decimal(28009), "new charges"),
        points=[
            PointsProgram(
                "小樹點(信用卡)",
                Decimal(3206),
                [PointsExpiry(Decimal(88), date(2026, 4, 30))],
                [PointsLine("本期兌/轉換", PointsKind.REDEEM, Decimal(-1))],
            )
        ],
    )
    monkeypatch.setattr(
        server_module, "parse_document", lambda *_, **__: ParseResult("hsbc.credit_card", "1", statement, [])
    )

    response = parse(channel, email_with(), "message/rfc822")

    assert (response.parser, response.parser_version) == ("hsbc.credit_card", "1")
    assert response.statement.total_due.value == "95730"
    assert response.statement.reconciliation.balanced
    shop, payment = response.statement.transactions
    assert (shop.amount.value, shop.original_amount.value, shop.original_currency) == ("28009", "780.00", "EUR")
    assert (shop.transaction_date.year, shop.transaction_date.month, shop.transaction_date.day) == (2026, 10, 8)
    assert shop.kind == pb.TRANSACTION_KIND_PURCHASE
    assert payment.kind == pb.TRANSACTION_KIND_PAYMENT
    assert not payment.HasField("posting_date")
    assert not payment.HasField("original_amount")
    [program] = response.statement.points
    assert (program.name, program.closing.value) == ("小樹點(信用卡)", "3206")
    [expiry] = program.expiries
    assert (expiry.amount.value, expiry.by.year, expiry.by.month, expiry.by.day) == ("88", 2026, 4, 30)
    [line] = program.lines
    assert (line.label, line.kind, line.amount.value) == ("本期兌/轉換", pb.POINTS_LINE_KIND_REDEEM, "-1")


def test_open_document_returns_the_email_and_its_attachments_decrypted_by_the_banks_rule() -> None:
    # Synthetic details (ADR 0006); Cathay United's rule is the national ID with a capital letter.
    settings = Settings(_env_file=None, ocr=False, workers=1, identity={"national_id": "a123456789"})
    server, port = server_module.build_server(settings, None, "127.0.0.1:0")
    server.start()
    try:
        with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
            stub = parser_pb2_grpc.ParserServiceStub(channel)
            request = pb.OpenDocumentRequest(
                document_id="d1",
                filename="mail.eml",
                content_type="message/rfc822",
                content=statement_email(
                    blank_pdf("A123456789"),
                    sender="國泰世華銀行 <service@pxbillrc01.cathaybk.com.tw>",
                    subject="國泰世華銀行信用卡2026年11月電子帳單",
                ),
            )
            response = stub.OpenDocument(request, timeout=10)
    finally:
        server.stop(grace=None)
    assert response.email.subject == "國泰世華銀行信用卡2026年11月電子帳單"
    assert response.email.date.ToDatetime(tzinfo=UTC) == datetime(2026, 11, 6, 1, 30, tzinfo=UTC)
    assert "data:image/png;base64," in response.email.html
    assert [(f.filename, f.content_type, f.locked, f.primary) for f in response.files] == [
        ("帳單_11511.pdf", "application/pdf", False, True),
        ("slip.html", "text/html", False, False),
    ]


def test_open_document_rejects_an_empty_document(channel: grpc.Channel) -> None:
    stub = parser_pb2_grpc.ParserServiceStub(channel)
    request = pb.OpenDocumentRequest(document_id="d1", filename="a.eml", content_type="message/rfc822")
    with pytest.raises(grpc.RpcError) as error:
        stub.OpenDocument(request, timeout=10)
    assert error.value.code() is grpc.StatusCode.INVALID_ARGUMENT
