"""Choosing the statement among an e-mail's PDFs (`select_statement`), directly; test_pipeline.py and
test_reading.py check how parsing and previews use the choice."""

import pytest
from fakes import FakeParser
from pdfs import blank_pdf, email_with, text_pdf

from finchie_parser.documents import read_mail
from finchie_parser.errors import InvalidDocumentError, PasswordRequiredError, UnsupportedDocumentError
from finchie_parser.passwords import Passwords
from finchie_parser.pipeline import parse_document
from finchie_parser.reading import open_document
from finchie_parser.selection import Found, Locked, NotFound, PdfSet, Selection, select_statement

STATEMENT = text_pdf("FAKE BANK credit card statement")
SLIP = text_pdf("FAKE BANK payment slip")
CORRUPT = b"%PDF-1.7 truncated"


def select(*attachments: tuple[str, bytes], parser: FakeParser | None = None, **mail: str) -> Selection:
    content = email_with(*((name, "application/pdf", data) for name, data in attachments), **mail)
    parsed = read_mail(content)
    with PdfSet(parsed.pdfs(), Passwords()) as pdfs:
        return select_statement(parsed, pdfs, [parser or FakeParser()])


def test_the_pdf_known_by_name_with_the_bank_in_its_text_is_found() -> None:
    selection = select(("PaymentSlip.pdf", SLIP), ("FAKE_1.pdf", STATEMENT))
    assert isinstance(selection, Found)
    assert (selection.index, selection.parser.name) == (1, "fake.statement")


def test_without_a_known_name_a_pdf_with_the_bank_in_its_text_is_found() -> None:
    selection = select(("notice.pdf", blank_pdf()), ("x.pdf", STATEMENT))
    assert isinstance(selection, Found)
    assert selection.index == 1


def test_a_statement_known_by_name_that_no_password_opens_is_locked_with_its_error() -> None:
    selection = select(("PaymentSlip.pdf", SLIP), ("FAKE_1.pdf", text_pdf("FAKE BANK", user_password="unknown")))
    assert isinstance(selection, Locked)
    assert selection.index == 1
    assert isinstance(selection.error, PasswordRequiredError)


def test_an_email_no_parser_claims_is_not_found_and_names_no_parser() -> None:
    selection = select(("FAKE_1.pdf", STATEMENT), sender="statements@elsewhere.example")
    assert isinstance(selection, NotFound)
    assert isinstance(selection.error, UnsupportedDocumentError)
    assert selection.parser is None


@pytest.mark.parametrize(
    ("attachments", "error"),
    [
        ((("other.pdf", blank_pdf()),), UnsupportedDocumentError),
        ((("broken.pdf", CORRUPT), ("other.pdf", blank_pdf())), InvalidDocumentError),
        ((("broken.pdf", CORRUPT), ("notice.pdf", blank_pdf("unknown"))), PasswordRequiredError),
    ],
    ids=["none-is-it", "unreadable", "locked-before-unreadable"],
)
def test_nothing_found_says_why_and_keeps_the_claiming_parser(
    attachments: tuple[tuple[str, bytes], ...], error: type[Exception]
) -> None:
    selection = select(*attachments)
    assert isinstance(selection, NotFound)
    assert isinstance(selection.error, error)
    assert selection.parser is not None
    assert selection.parser.name == "fake.statement"


def test_parsing_and_previews_choose_from_the_same_pdfs_whatever_they_are_declared_as() -> None:
    # A statement sent as application/octet-stream under a name without .pdf, beside another PDF: a PDF by
    # its bytes, for parsing as for previews (ADR 0012).
    content = email_with(
        ("statement", "application/octet-stream", STATEMENT), ("other.pdf", "application/pdf", blank_pdf())
    )
    parser = FakeParser()
    parse_document(content, passwords=Passwords(), ocr=None, parsers=[parser])
    opened = open_document(content, passwords=Passwords(), parsers=[FakeParser()])
    assert parser.parsed == ["statement"]
    assert [f.filename for f in opened.files if f.primary] == ["statement"]
