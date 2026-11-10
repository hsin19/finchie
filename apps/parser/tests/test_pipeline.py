import re
from collections.abc import Callable

import pytest
from fakes import FAKE_STATEMENT, FakeOcr, FakeParser
from pdfs import attached_forward, blank_pdf, email_with, gmail_forward, text_pdf

from finchie_parser.errors import (
    AbandonedError,
    ConfigurationError,
    InvalidDocumentError,
    PasswordRequiredError,
    UnsupportedDocumentError,
)
from finchie_parser.model import ParseResult
from finchie_parser.ocr import Ocr
from finchie_parser.parsers import Parsed, StatementParser
from finchie_parser.passwords import Identity, Passwords
from finchie_parser.pdf import Document
from finchie_parser.pipeline import parse_document

STATEMENT_PDF = text_pdf("FAKE BANK credit card statement")
SLIP_PDF = text_pdf("FAKE BANK payment slip")
CORRUPT_PDF = b"%PDF-1.7 truncated"


def run(
    content: bytes, *, parser: FakeParser | None = None, passwords: Passwords | None = None
) -> tuple[ParseResult, FakeParser]:
    parser = parser or FakeParser()
    result = parse_document(content, passwords=passwords or Passwords(), ocr=None, parsers=[parser])
    return result, parser


def pdf(name: str, data: bytes) -> tuple[str, str, bytes]:
    return (name, "application/pdf", data)


def test_known_attachment_name_wins_over_other_pdfs() -> None:
    result, parser = run(email_with(pdf("PaymentSlip.pdf", SLIP_PDF), pdf("FAKE_202604.pdf", STATEMENT_PDF)))
    assert parser.parsed == ["FAKE_202604.pdf"]
    assert (result.parser, result.parser_version, result.warnings) == ("fake.statement", "7", ["a warning"])
    assert result.statement is FAKE_STATEMENT


def test_an_email_no_parser_claims_is_unsupported_even_with_its_statement_attached() -> None:
    with pytest.raises(UnsupportedDocumentError, match="no parser claims"):
        run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF), sender="statements@elsewhere.example"))
    with pytest.raises(UnsupportedDocumentError, match="no parser claims"):
        run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF), subject="Our new card"))


def test_a_subdomain_is_claimed_but_a_lookalike_domain_is_not() -> None:
    _, parser = run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF), sender="no_reply@mail.bank.example"))
    assert parser.parsed == ["FAKE_1.pdf"]
    with pytest.raises(UnsupportedDocumentError):
        run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF), sender="statements@fakebank.example"))


def test_the_subject_tells_two_kinds_of_statement_from_one_sender_apart() -> None:
    class CardParser(FakeParser):
        name = "fake.credit_card"
        subject_terms = ("信用卡電子帳單",)

    class DepositParser(FakeParser):
        name = "fake.deposit"
        subject_terms = ("綜合對帳單",)

    parsers: list[StatementParser] = [CardParser(), DepositParser()]
    content = email_with(pdf("FAKE_1.pdf", STATEMENT_PDF), subject="範例銀行2026年09月綜合對帳單")
    result = parse_document(content, passwords=Passwords(), ocr=None, parsers=parsers)
    assert result.parser == "fake.deposit"


def test_a_pdf_named_like_the_statement_without_the_bank_in_its_text_is_not_it() -> None:
    with pytest.raises(UnsupportedDocumentError, match="none of its PDFs is a statement"):
        run(email_with(pdf("FAKE_202604.pdf", text_pdf("OTHER BANK statement"))))


def test_an_unnamed_statement_is_recognized_by_its_text() -> None:
    _, parser = run(email_with(pdf("notice.pdf", blank_pdf()), pdf("x.pdf", STATEMENT_PDF)))
    assert parser.parsed == ["x.pdf"]


@pytest.mark.parametrize("unreadable", [blank_pdf("other"), CORRUPT_PDF], ids=["locked", "corrupt"])
def test_an_unreadable_pdf_does_not_hide_the_statement_after_it(unreadable: bytes) -> None:
    _, parser = run(email_with(pdf("notice.pdf", unreadable), pdf("statement.pdf", STATEMENT_PDF)))
    assert parser.parsed == ["statement.pdf"]


def test_an_encrypted_statement_opens_with_the_banks_rule() -> None:
    content = email_with(pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="A123456789")))
    parser = FakeParser(lambda identity: [identity.require_national_id().upper()])
    result, _ = run(content, parser=parser, passwords=Passwords(Identity(national_id="a123456789")))
    assert result.statement is FAKE_STATEMENT


def test_a_password_the_owner_chose_opens_it_and_older_statements_still_open_with_the_rule() -> None:
    passwords = Passwords(chosen={"fake": "mine"})
    for password in ("mine", "secret"):
        content = email_with(pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password=password)))
        result, _ = run(content, passwords=passwords)
        assert result.statement is FAKE_STATEMENT


def test_another_banks_password_is_never_tried() -> None:
    content = email_with(pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="theirs")))
    with pytest.raises(PasswordRequiredError, match="PARSER_PDF_PASSWORDS__FAKE"):
        run(content, passwords=Passwords(chosen={"other": "theirs"}))


def test_a_rule_without_its_detail_says_which_one_to_set() -> None:
    content = email_with(pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="A123456789")))
    parser = FakeParser(lambda identity: [identity.require_national_id()])
    with pytest.raises(PasswordRequiredError, match=re.escape("fake.statement needs PARSER_IDENTITY__NATIONAL_ID")):
        run(content, parser=parser)


def test_an_unencrypted_statement_needs_no_detail() -> None:
    parser = FakeParser(lambda identity: [identity.require_national_id()])
    result, _ = run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF)), parser=parser)
    assert result.statement is FAKE_STATEMENT


def test_a_locked_statement_by_name_needs_its_password_even_beside_a_slip_that_names_the_bank() -> None:
    content = email_with(
        pdf("PaymentSlip.pdf", SLIP_PDF), pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="unknown"))
    )
    with pytest.raises(PasswordRequiredError):
        run(content)


def test_nothing_recognized_reports_a_locked_pdf_before_a_corrupt_one() -> None:
    corrupt = pdf("broken.pdf", CORRUPT_PDF)
    locked = pdf("notice.pdf", blank_pdf("unknown"))
    with pytest.raises(PasswordRequiredError):
        run(email_with(corrupt, locked, pdf("other.pdf", blank_pdf())))
    with pytest.raises(InvalidDocumentError):
        run(email_with(corrupt, pdf("other.pdf", blank_pdf())))


def test_a_claimed_email_without_a_pdf_is_unsupported() -> None:
    with pytest.raises(UnsupportedDocumentError):
        run(email_with())


def test_a_bare_pdf_is_invalid() -> None:
    with pytest.raises(InvalidDocumentError, match="bare PDF"):
        run(STATEMENT_PDF)


@pytest.mark.parametrize("forward", [gmail_forward, attached_forward], ids=["gmail-inline", "as-attachment"])
def test_a_forwarded_statement_is_parsed_as_the_banks_email(forward: Callable[[bytes], bytes]) -> None:
    original = email_with(pdf("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="secret")))
    _, parser = run(forward(original))
    assert parser.parsed == ["FAKE_1.pdf"]


def test_a_statement_whose_parser_needs_ocr_fails_as_configuration_while_ocr_is_off() -> None:
    class OcrParser(FakeParser):
        needs_ocr = True

    parser = OcrParser()
    with pytest.raises(ConfigurationError, match="PARSER_OCR"):
        run(email_with(pdf("FAKE_1.pdf", STATEMENT_PDF)), parser=parser)
    assert parser.parsed == []


def test_a_parse_the_caller_no_longer_waits_for_stops_before_parsing() -> None:
    parser = FakeParser()
    content = email_with(pdf("FAKE_1.pdf", STATEMENT_PDF))
    with pytest.raises(AbandonedError):
        parse_document(content, passwords=Passwords(), ocr=None, parsers=[parser], wanted=lambda: False)
    assert parser.parsed == []


def test_a_parse_the_caller_stops_waiting_for_stops_before_its_next_ocr_call() -> None:
    waiting = [True]

    class OcrParser(FakeParser):
        def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
            assert ocr is not None
            ocr.read_column(document, 0, 0, 100)
            waiting[0] = False
            ocr.read_column(document, 1, 0, 100)
            raise AssertionError("read past the caller's deadline")

    ocr = FakeOcr()
    content = email_with(pdf("FAKE_1.pdf", STATEMENT_PDF))
    with pytest.raises(AbandonedError):
        parse_document(content, passwords=Passwords(), ocr=ocr, parsers=[OcrParser()], wanted=lambda: waiting[0])
