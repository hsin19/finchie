"""Read an upload as the e-mail the bank sent: unwrapped from a forward, with its PDFs (ADR 0014).

Whoever can upload chooses what the parser spends its time and memory on (ADR 0010), so an e-mail with more
parts than MAX_PARTS, or forwards nested deeper than MAX_NESTING, is refused before anything reads it.
"""

import email
import email.policy
import html
import re
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import datetime, tzinfo
from email.message import EmailMessage, MIMEPart
from email.utils import parseaddr

from finchie_parser.errors import InvalidDocumentError

PDF = "application/pdf"
# Far more than any bank's e-mail has, forwarded or not.
MAX_PARTS = 200
MAX_NESTING = 5


@dataclass(frozen=True)
class Attachment:
    filename: str
    data: bytes


@dataclass(frozen=True)
class Mail:
    """The bank's e-mail. `content` holds its body and attachments: the bank's own message, or, for an
    inline forward, the forward, whose attachments are the bank's."""

    sender: str
    recipient: str
    subject: str
    date: datetime | None
    content: EmailMessage

    @property
    def sender_domain(self) -> str:
        return parseaddr(self.sender)[1].rpartition("@")[2].lower()

    def pdfs(self) -> list[Attachment]:
        """Every part that is a PDF (`pdf_data`), in order: the PDFs parsing and previews choose from."""
        return [
            Attachment(part.get_filename() or "attachment.pdf", data)
            for part in self.content.walk()
            if (data := pdf_data(part)) is not None
        ]


def pdf_data(part: EmailMessage) -> bytes | None:
    """The part's bytes if they are a PDF. By the bytes alone: banks send PDFs as application/octet-stream
    as often as application/pdf, and a type or a name can claim a PDF the bytes are not."""
    if part.is_multipart():
        return None
    data = part.get_payload(decode=True)
    return data if isinstance(data, bytes) and data.startswith(b"%PDF") else None


def read_mail(content: bytes) -> Mail:
    if not content:
        raise InvalidDocumentError("document is empty")
    if content.startswith(b"%PDF"):
        raise InvalidDocumentError("a bare PDF: statements are uploaded as their e-mails (ADR 0014)")
    message = email.message_from_bytes(content, policy=email.policy.default)
    if not isinstance(message, EmailMessage) or not message.keys():
        raise InvalidDocumentError("not an e-mail message")
    _check_size(message)
    return unwrap(message)


def _check_size(message: EmailMessage) -> None:
    """Refuses an e-mail past MAX_PARTS or MAX_NESTING, walking it without recursion."""
    parts = 0
    stack: list[tuple[EmailMessage, int]] = [(message, 0)]
    while stack:
        part, depth = stack.pop()
        parts += 1
        if parts > MAX_PARTS:
            raise InvalidDocumentError(f"an e-mail of more than {MAX_PARTS} parts")
        if part.get_content_type() == "message/rfc822":
            depth += 1
            if depth > MAX_NESTING:
                raise InvalidDocumentError(f"messages nested more than {MAX_NESTING} deep")
        if part.is_multipart():
            stack.extend((subpart, depth) for subpart in part.get_payload() if isinstance(subpart, EmailMessage))


def unwrap(message: EmailMessage) -> Mail:
    """The innermost e-mail: forwarded as an attachment, or inline as Gmail forwards."""
    for part in message.walk():
        if part is not message and part.get_content_type() == "message/rfc822":
            inner = part.get_content()
            if isinstance(inner, EmailMessage):
                return unwrap(inner)
    date = getattr(message["Date"], "datetime", None)
    if (fields := _inline_forward(message)) is not None:
        return Mail(
            sender=fields.get("from", ""),
            recipient=fields.get("to", ""),
            subject=fields.get("subject", ""),
            # Gmail writes the date in the forwarder's time zone, the one its own Date header has.
            date=_forwarded_date(fields.get("date", ""), date.tzinfo if date else None),
            content=message,
        )
    return Mail(
        sender=str(message["From"] or ""),
        recipient=str(message["To"] or ""),
        subject=str(message["Subject"] or ""),
        date=date,
        content=message,
    )


# Gmail on the web writes the marker in English whatever its language; the Gmail app in the forwarder's.
_FORWARD_MARKER = re.compile(r"-{5,}\s*(?:Forwarded message|轉寄的郵件|转发的邮件)\s*-{5,}")
# Gmail writes the labels in the forwarder's language, and not all of them in it.
_LABELS = {
    "from": "from",
    "寄件者": "from",
    "to": "to",
    "收件者": "to",
    "subject": "subject",
    "主旨": "subject",
    "date": "date",
    "日期": "date",
    "cc": "cc",
    "副本": "cc",
}
_HEADER_LINE = re.compile(r"^\s*([^:：]{1,10}?)\s*[:：]\s*(.*)$")


def _inline_forward(message: EmailMessage) -> dict[str, str] | None:
    """The header block of the last forwarded message in the body, the original. None if not a forward."""
    text = _plain_text(message)
    markers = list(_FORWARD_MARKER.finditer(text))
    if not markers:
        return None
    fields: dict[str, str] = {}
    last = ""
    for line in text[markers[-1].end() :].splitlines()[1:]:
        if not line.strip():
            if fields:
                break
            continue
        match = _HEADER_LINE.match(line)
        if match and (name := _LABELS.get(match.group(1).strip().lower())):
            fields[name] = match.group(2).strip()
            last = name
        elif last:
            # A long value wrapped onto the next line.
            fields[last] = f"{fields[last]} {line.strip()}"
        else:
            break
    return fields if "from" in fields else None


_ZH_DATE = re.compile(r"(\d{4})年(\d{1,2})月(\d{1,2})日.*?(上午|下午)\s*(\d{1,2}):(\d{2})")
_EN_DATE = re.compile(r"(\w{3} \d{1,2}, \d{4}) at (\d{1,2}:\d{2}\s*[AP]M)")


def _forwarded_date(value: str, zone: tzinfo | None) -> datetime | None:
    if zone is None:
        return None
    if match := _ZH_DATE.search(value):
        year, month, day, half, hour, minute = match.groups()
        hour_24 = int(hour) % 12 + (12 if half == "下午" else 0)
        return datetime(int(year), int(month), int(day), hour_24, int(minute), tzinfo=zone)
    if match := _EN_DATE.search(value):
        moment = f"{match.group(1)} {match.group(2).replace(' ', '')}"
        return datetime.strptime(moment, "%b %d, %Y %I:%M%p").replace(tzinfo=zone)
    return None


def _plain_text(message: EmailMessage) -> str:
    candidates = list(body_candidates(message))
    if plain := next((part for part in candidates if part.get_content_subtype() == "plain"), None):
        return text_of(plain)
    if rich := next((part for part in candidates if part.get_content_subtype() == "html"), None):
        markup = re.sub(r"<br\s*/?>|</(div|p|tr)>", "\n", text_of(rich), flags=re.IGNORECASE)
        return html.unescape(re.sub(r"<[^>]+>", "", markup))
    return ""


def is_attachment(part: MIMEPart) -> bool:
    # Python's is_attachment() counts only "Content-Disposition: attachment"; a named part marked inline is a file too.
    return part.is_attachment() or bool(part.get_filename())


def find_body(message: MIMEPart) -> MIMEPart | None:
    """HTML, else plain text, searched for as `EmailMessage.get_body` does, but never a file."""
    candidates = list(body_candidates(message))
    for subtype in ("html", "plain"):
        if body := next((part for part in candidates if part.get_content_subtype() == subtype), None):
            return body
    return None


def body_candidates(part: MIMEPart) -> Iterator[MIMEPart]:
    if is_attachment(part):
        return
    if part.get_content_maintype() == "text":
        yield part
    elif part.get_content_maintype() == "multipart":
        subparts = list(part.iter_parts())
        if part.get_content_subtype() == "related" and subparts:
            # Only its root is shown, the part `start` names or else the first; the rest are its images.
            start = part.get_param("start")
            subparts = [next((p for p in subparts if start and p["Content-ID"] == start), subparts[0])]
        for subpart in subparts:
            yield from body_candidates(subpart)


def text_of(part: MIMEPart) -> str:
    try:
        return str(part.get_content())
    except LookupError:
        # A charset Python does not know: most of the text is still readable as UTF-8.
        payload = part.get_payload(decode=True)
        return payload.decode("utf-8", errors="replace") if isinstance(payload, bytes) else ""
