"""Open a document for a person to read: an e-mail's headers, body, and attachments, PDFs decrypted."""

import base64
import dataclasses
import mimetypes
import re
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime
from email.message import EmailMessage

from finchie_parser.documents import PDF, Attachment, Mail, find_body, is_attachment, read_mail, text_of
from finchie_parser.errors import InvalidDocumentError, ParserError, PasswordRequiredError
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.passwords import Passwords
from finchie_parser.pdf import PdfSet
from finchie_parser.pipeline import claimants, select_statement

# They sign the message; there is nothing in them to read.
_SIGNATURES = frozenset({"application/pkcs7-signature", "application/x-pkcs7-signature", "application/pgp-signature"})
_CID = re.compile(r"cid:([^\"'\s)>]+)", re.IGNORECASE)


@dataclass(frozen=True)
class File:
    filename: str
    content_type: str
    data: bytes
    locked: bool = False
    primary: bool = False


@dataclass(frozen=True)
class Email:
    sender: str
    recipient: str
    subject: str
    date: datetime | None
    html: str = ""
    text: str = ""


@dataclass(frozen=True)
class OpenedDocument:
    email: Email
    files: list[File]


def open_document(
    content: bytes,
    *,
    passwords: Passwords,
    parsers: Sequence[StatementParser] = PARSERS,
) -> OpenedDocument:
    """The bank's e-mail, unwrapped from a forward: the bank's headers, and the body and files that came."""
    mail = read_mail(content)
    message = mail.content
    body = find_body(message)
    inline = {
        str(part["Content-ID"]).strip("<> "): part
        for part in message.walk()
        if not part.is_multipart() and part["Content-ID"]
    }
    html = text = ""
    embedded: set[str] = set()
    if body is not None and body.get_content_subtype() == "html":
        html = _CID.sub(lambda m: _data_uri(m, inline, embedded), text_of(body))
    elif body is not None:
        text = text_of(body)

    files = []
    for part in message.walk():
        if part.is_multipart() or part is body or part.get_content_type() in _SIGNATURES:
            continue
        if str(part["Content-ID"] or "").strip("<> ") in embedded:
            continue
        # The plain-text twin of an HTML body, or the body of a forwarded message inside this one.
        if part.get_content_maintype() == "text" and not is_attachment(part):
            continue
        files.append(_file(part))

    return OpenedDocument(
        Email(sender=mail.sender, recipient=mail.recipient, subject=mail.subject, date=mail.date, html=html, text=text),
        _open_pdfs(mail, files, passwords, parsers),
    )


def _open_pdfs(mail: Mail, files: list[File], passwords: Passwords, parsers: Sequence[StatementParser]) -> list[File]:
    """PDFs decrypted with the passwords of the parser that claims the e-mail, and primary the one parsing
    would take as the statement."""
    indexes = [i for i, f in enumerate(files) if f.content_type == PDF]
    opened = list(files)
    claimed = claimants(mail, parsers)
    parser = claimed[0] if claimed else None
    with PdfSet([Attachment(files[i].filename, files[i].data) for i in indexes], passwords) as pdfs:
        try:
            primary, parser = select_statement(mail, pdfs, parsers)
        except ParserError:
            primary = 0  # none is a statement: the first PDF stands for the document
        for n, i in enumerate(indexes):
            try:
                opened[i] = dataclasses.replace(files[i], data=pdfs.decrypted(n, parser), primary=n == primary)
            except PasswordRequiredError:
                opened[i] = dataclasses.replace(files[i], locked=True, primary=n == primary)
            except InvalidDocumentError:
                # Shown as it arrived: the browser's PDF viewer says what is wrong with it.
                opened[i] = dataclasses.replace(files[i], primary=n == primary)
    return opened


def _file(part: EmailMessage) -> File:
    data = part.get_payload(decode=True)
    if not isinstance(data, bytes):
        data = b""
    name = part.get_filename()
    if data.startswith(b"%PDF"):
        return File(name or "attachment.pdf", PDF, data)
    content_type = part.get_content_type()
    # Only bytes that are a PDF may be called one: the app shows a PDF in place on its own origin.
    if content_type == PDF:
        content_type = "application/octet-stream"
    return File(name or f"attachment{mimetypes.guess_extension(content_type) or ''}", content_type, data)


def _data_uri(match: re.Match[str], inline: dict[str, EmailMessage], embedded: set[str]) -> str:
    part = inline.get(match.group(1))
    data = part.get_payload(decode=True) if part is not None else None
    # Only images: anything else it links to, a PDF say, stays a file of its own.
    if part is None or part.get_content_maintype() != "image" or not isinstance(data, bytes):
        return match.group(0)
    embedded.add(match.group(1))
    return f"data:{part.get_content_type()};base64,{base64.b64encode(data).decode('ascii')}"
