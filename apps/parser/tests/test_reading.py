import io
from datetime import datetime, timedelta, timezone
from email.message import EmailMessage

import pikepdf
import pytest
from fakes import FakeParser
from pdfs import PNG, blank_pdf, email_with, gmail_forward, statement_email, text_pdf

from finchie_parser import reading
from finchie_parser.errors import InvalidDocumentError
from finchie_parser.passwords import Passwords
from finchie_parser.reading import File, OpenedDocument, open_document


def opened(content: bytes, rule: list[str] | None = None) -> OpenedDocument:
    """Opened as FakeParser, which claims bank.example's e-mails, would with `rule` as its passwords."""
    parser = FakeParser(lambda _: rule or [])
    return open_document(content, passwords=Passwords(), parsers=[parser])


def opens_without_password(data: bytes) -> bool:
    try:
        pikepdf.open(io.BytesIO(data)).close()
    except pikepdf.PasswordError:
        return False
    return True


def test_email_shows_headers_html_body_with_inline_images_and_its_attachments() -> None:
    document = opened(statement_email(blank_pdf("secret")), ["secret"])

    mail = document.email
    assert mail.sender == "範例銀行 <statements@bank.example>"
    assert mail.recipient == "someone@example.com"
    assert mail.subject == "範例銀行 2026年11月 信用卡電子帳單"
    assert mail.date == datetime(2026, 11, 6, 9, 30, tzinfo=timezone(timedelta(hours=8)))
    assert "本期帳單" in mail.html
    assert "cid:" not in mail.html
    assert "data:image/png;base64," in mail.html
    assert mail.text == ""

    assert [(f.filename, f.content_type, f.locked) for f in document.files] == [
        ("帳單_11511.pdf", "application/pdf", False),
        ("slip.html", "text/html", False),
    ]
    assert opens_without_password(document.files[0].data)
    assert document.files[1].data == b"<html>slip</html>"


def test_a_pdf_none_of_the_claiming_parsers_passwords_opens_comes_back_locked_as_received() -> None:
    pdf = blank_pdf("secret")
    document = opened(statement_email(pdf), ["wrong"])
    assert document.files[0].locked
    assert document.files[0].data == pdf


def test_a_pdf_in_an_email_no_parser_claims_is_opened_only_without_a_password() -> None:
    content = statement_email(blank_pdf("secret"), sender="statements@elsewhere.example")
    assert opened(content, ["secret"]).files[0].locked
    assert not opened(statement_email(blank_pdf(), sender="statements@elsewhere.example")).files[0].locked


def test_a_forwarded_email_shows_the_banks_headers_and_opens_with_its_parsers_passwords() -> None:
    document = opened(gmail_forward(statement_email(blank_pdf("secret"))), ["secret"])
    assert document.email.sender == "範例銀行 <statements@bank.example>"
    assert document.email.subject == "範例銀行 2026年11月 信用卡電子帳單"
    assert [(f.filename, f.locked) for f in document.files if f.content_type == "application/pdf"] == [
        ("帳單_11511.pdf", False)
    ]


def test_plain_text_email_has_a_text_body() -> None:
    message = EmailMessage()
    message["From"] = "statements@bank.example"
    message["Subject"] = "Statement"
    message.set_content("Your statement is attached.")
    document = opened(message.as_bytes())
    assert document.email.text.strip() == "Your statement is attached."
    assert document.email.html == ""
    assert document.email.date is None
    assert document.files == []


def test_corrupt_pdf_is_shown_as_it_arrived() -> None:
    document = opened(email_with(("broken.pdf", "application/pdf", b"%PDF-1.7 truncated")))
    assert document.files[0].data == b"%PDF-1.7 truncated"
    assert not document.files[0].locked


@pytest.mark.parametrize("content", [b"", blank_pdf(), PNG], ids=["empty", "bare-pdf", "other-bytes"])
def test_anything_but_an_email_is_invalid(content: bytes) -> None:
    with pytest.raises(InvalidDocumentError):
        opened(content)


def primary(files: list[File]) -> list[str]:
    return [f.filename for f in files if f.primary]


STATEMENT = text_pdf("FAKE BANK credit card statement")
SLIP = text_pdf("FAKE BANK payment slip")


def mail(*attachments: tuple[str, bytes]) -> list[File]:
    return opened(email_with(*((name, "application/pdf", data) for name, data in attachments))).files


def test_a_pdf_named_like_a_statement_without_its_bank_in_the_text_is_not_primary() -> None:
    assert primary(mail(("other.pdf", STATEMENT), ("FAKE_202610.pdf", blank_pdf()))) == ["other.pdf"]


def test_a_locked_statement_by_name_is_primary_so_the_viewer_asks_for_its_password() -> None:
    files = mail(("PaymentSlip.pdf", SLIP), ("FAKE_202610.pdf", text_pdf("FAKE BANK", user_password="secret")))
    assert [(f.filename, f.locked) for f in files if f.primary] == [("FAKE_202610.pdf", True)]


def test_without_a_known_name_the_first_pdf_is_primary() -> None:
    pdf = blank_pdf()
    content = email_with(
        ("notice.html", "text/html", b"<p>hi</p>"), ("a.pdf", "application/pdf", pdf), ("b.pdf", "application/pdf", pdf)
    )
    assert primary(opened(content).files) == ["a.pdf"]


def test_an_email_without_pdfs_has_no_primary() -> None:
    assert primary(opened(email_with(("notice.html", "text/html", b"<p>hi</p>"))).files) == []


def test_bytes_declared_as_pdf_that_are_not_one_are_neither_called_pdf_nor_primary() -> None:
    content = email_with(
        ("statement.pdf", "application/pdf", b"<html>not a pdf</html>"), ("b.pdf", "application/pdf", blank_pdf())
    )
    files = opened(content).files
    assert [(f.filename, f.content_type, f.primary) for f in files] == [
        ("statement.pdf", "application/octet-stream", False),
        ("b.pdf", "application/pdf", True),
    ]


def test_a_text_part_with_a_file_name_is_an_attachment_even_marked_inline() -> None:
    message = EmailMessage()
    message["From"] = "statements@bank.example"
    message.set_content("Your statement is attached.")
    message.add_attachment(
        b"<p>statement</p>", maintype="text", subtype="html", filename="statement.html", disposition="inline"
    )
    document = opened(message.as_bytes())
    assert document.email.text.strip() == "Your statement is attached."
    assert document.email.html == ""
    assert [(f.filename, f.content_type, f.data) for f in document.files] == [
        ("statement.html", "text/html", b"<p>statement</p>")
    ]


def test_only_images_are_inlined_and_a_pdf_the_html_links_by_content_id_stays_a_file() -> None:
    message = EmailMessage()
    message["From"] = "statements@bank.example"
    message.set_content("Plain version of the body.")
    message.add_alternative(
        '<img src="cid:logo@bank.example"><a href="cid:statement@bank.example">statement</a>', subtype="html"
    )
    html = message.get_body(("html",))
    assert html is not None
    html.add_related(PNG, maintype="image", subtype="png", cid="<logo@bank.example>")
    html.add_related(
        blank_pdf(), maintype="application", subtype="pdf", cid="<statement@bank.example>", filename="statement.pdf"
    )

    document = opened(message.as_bytes())

    assert "data:image/png;base64," in document.email.html
    assert "data:application/pdf" not in document.email.html
    assert [(f.filename, f.content_type, f.primary) for f in document.files] == [
        ("statement.pdf", "application/pdf", True)
    ]


def test_an_image_the_body_refers_to_too_often_stops_being_inlined(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(reading, "MAX_INLINED_BYTES", 3 * len(PNG))
    message = EmailMessage()
    message["From"] = "statements@bank.example"
    message.set_content("Plain version of the body.")
    message.add_alternative('<img src="cid:logo@bank.example">' * 5, subtype="html")
    html = message.get_body(("html",))
    assert html is not None
    html.add_related(PNG, maintype="image", subtype="png", cid="<logo@bank.example>")

    document = opened(message.as_bytes())

    assert document.email.html.count("data:image/png;base64,") == 3
    assert document.email.html.count("cid:logo@bank.example") == 2
