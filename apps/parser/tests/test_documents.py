from datetime import datetime, timedelta, timezone

import pytest
from pdfs import OWNER, PNG, attached_forward, blank_pdf, email_with, gmail_forward, statement_email

from finchie_parser import documents
from finchie_parser.documents import read_mail
from finchie_parser.errors import InvalidDocumentError

TAIPEI = timezone(timedelta(hours=8))
BANK = "範例銀行 <statements@bank.example>"
SUBJECT = "範例銀行 2026年11月 信用卡電子帳單"


def test_an_email_has_the_banks_headers() -> None:
    mail = read_mail(statement_email(blank_pdf()))
    assert (mail.sender, mail.recipient, mail.subject) == (BANK, "someone@example.com", SUBJECT)
    assert mail.sender_domain == "bank.example"
    assert mail.date == datetime(2026, 11, 6, 9, 30, tzinfo=TAIPEI)


def test_an_email_yields_pdf_attachments_including_octet_stream_and_encoded_names() -> None:
    pdf = blank_pdf()
    content = email_with(
        ("信用卡電子帳單消費明細_11510.pdf", "application/octet-stream", pdf),
        ("eBarCodeFullPay.html", "text/html", b"<html></html>"),
        ("slip.pdf", "application/pdf", pdf),
    )
    assert [a.filename for a in read_mail(content).pdfs()] == ["信用卡電子帳單消費明細_11510.pdf", "slip.pdf"]


def test_an_email_without_pdfs_yields_nothing() -> None:
    assert read_mail(email_with()).pdfs() == []


@pytest.mark.parametrize(
    ("content", "message"),
    [(b"", "empty"), (blank_pdf(), "bare PDF"), (PNG, "not an e-mail")],
    ids=["empty", "bare-pdf", "other-bytes"],
)
def test_anything_but_an_email_is_invalid(content: bytes, message: str) -> None:
    with pytest.raises(InvalidDocumentError, match=message):
        read_mail(content)


@pytest.mark.parametrize(
    "options", [{}, {"english": True}, {"html_only": True}], ids=["zh-labels", "en-labels", "html-only"]
)
def test_a_gmail_forward_reads_as_the_banks_email_with_the_forwards_attachments(options: dict[str, bool]) -> None:
    mail = read_mail(gmail_forward(statement_email(blank_pdf()), **options))
    assert (mail.sender, mail.subject) == (BANK, SUBJECT)
    assert mail.recipient == "<someone@example.com>"
    assert mail.sender_domain == "bank.example"
    # Written in the forwarder's time zone, which its own Date header carries.
    assert mail.date == datetime(2026, 11, 6, 21, 30, tzinfo=TAIPEI)
    assert [a.filename for a in mail.pdfs()] == ["帳單_11511.pdf"]


def test_a_forward_of_a_forward_reads_as_the_innermost() -> None:
    mail = read_mail(gmail_forward(gmail_forward(statement_email(blank_pdf()))))
    assert (mail.sender, mail.subject) == (BANK, SUBJECT)


def test_a_forward_as_an_attachment_is_the_attached_email() -> None:
    mail = read_mail(attached_forward(statement_email(blank_pdf())))
    assert (mail.sender, mail.subject) == (BANK, SUBJECT)
    assert [a.filename for a in mail.pdfs()] == ["帳單_11511.pdf"]


def test_a_body_that_only_mentions_forwarding_is_not_a_forward() -> None:
    content = email_with(sender=OWNER, subject="Notes")
    content = content.replace(b"See attachment.", b"---------- Forwarded message ---------\nno headers here")
    mail = read_mail(content)
    assert (mail.sender, mail.subject) == (OWNER, "Notes")


def test_an_email_of_more_parts_than_any_bank_sends_is_refused_unread() -> None:
    attachments = [(f"{n}.txt", "text/plain", b"x") for n in range(documents.MAX_PARTS)]
    with pytest.raises(InvalidDocumentError, match="parts"):
        read_mail(email_with(*attachments))


def test_forwards_nested_deeper_than_any_owner_forwards_are_refused_unread() -> None:
    content = email_with(("FAKE_1.pdf", "application/pdf", blank_pdf()))
    for _ in range(documents.MAX_NESTING):
        content = attached_forward(content)
    read_mail(content)
    with pytest.raises(InvalidDocumentError, match="nested"):
        read_mail(attached_forward(content))


def test_a_pdf_is_known_by_its_bytes_whatever_its_type_or_name_says() -> None:
    mail = read_mail(
        email_with(
            ("statement", "application/octet-stream", blank_pdf()),
            ("fake.pdf", "application/pdf", b"<html>not a pdf</html>"),
        )
    )
    assert [attachment.filename for attachment in mail.pdfs()] == ["statement"]
