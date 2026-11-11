"""Open a document for a person to read: an e-mail's headers, body, and attachments, PDFs decrypted."""

import base64
import dataclasses
import mimetypes
import re
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime
from email.message import EmailMessage

from finchie_parser.documents import PDF, Mail, find_body, is_attachment, pdf_data, read_mail, text_of
from finchie_parser.errors import InvalidDocumentError, PasswordRequiredError
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.passwords import Passwords
from finchie_parser.selection import Found, Locked, NotFound, PdfSet, select_statement

# They sign the message; there is nothing in them to read.
_SIGNATURES = frozenset({"application/pkcs7-signature", "application/x-pkcs7-signature", "application/pgp-signature"})
_CID = re.compile(r"cid:([^\"'\s)>]+)", re.IGNORECASE)
# Images inlined into the body, in all: an image the body refers to many times is inlined each time.
# Past this, the rest stay links to nothing rather than multiply the response.
MAX_INLINED_BYTES = 8 * 1024 * 1024


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
    embedded: dict[str, int] = {}
    if body is not None and body.get_content_subtype() == "html":
        html = _CID.sub(lambda m: _data_uri(m, inline, embedded), text_of(body))
    elif body is not None:
        text = text_of(body)

    files = []
    for part in message.walk():
        if part.is_multipart():
            continue
        # First, so that the PDFs here are exactly those parsing chooses from (`Mail.pdfs`).
        if (data := pdf_data(part)) is not None:
            files.append(File(part.get_filename() or "attachment.pdf", PDF, data))
            continue
        if part is body or part.get_content_type() in _SIGNATURES:
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
    would take as the statement, even locked, so that the viewer asks for its password; the first PDF when
    none is."""
    indexes = [i for i, f in enumerate(files) if f.content_type == PDF]
    opened = list(files)
    with PdfSet(mail.pdfs(), passwords) as pdfs:
        if len(pdfs) != len(indexes):
            raise AssertionError("the files' PDFs are not the e-mail's")
        match select_statement(mail, pdfs, parsers):
            case Found(primary, parser) | Locked(primary, parser):
                pass
            case NotFound(parser=parser):
                primary = 0
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
    """A part that is not a PDF (`pdf_data`)."""
    data = part.get_payload(decode=True)
    if not isinstance(data, bytes):
        data = b""
    content_type = part.get_content_type()
    # Only bytes that are a PDF may be called one: the app shows a PDF in place on its own origin.
    if content_type == PDF:
        content_type = "application/octet-stream"
    return File(part.get_filename() or f"attachment{mimetypes.guess_extension(content_type) or ''}", content_type, data)


def _data_uri(match: re.Match[str], inline: dict[str, EmailMessage], embedded: dict[str, int]) -> str:
    """The image a `cid:` link names, inlined; `embedded` counts the bytes inlined so far by content ID."""
    part = inline.get(match.group(1))
    data = part.get_payload(decode=True) if part is not None else None
    # Only images: anything else it links to, a PDF say, stays a file of its own.
    if part is None or part.get_content_maintype() != "image" or not isinstance(data, bytes):
        return match.group(0)
    if sum(embedded.values()) + len(data) > MAX_INLINED_BYTES:
        return match.group(0)
    embedded[match.group(1)] = embedded.get(match.group(1), 0) + len(data)
    return f"data:{part.get_content_type()};base64,{base64.b64encode(data).decode('ascii')}"
