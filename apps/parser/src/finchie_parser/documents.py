"""Read an upload as the e-mail the bank sent: unwrapped from a forward, with its PDFs (ADR 0014)."""

import email
import email.policy
import html
import re
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import datetime, tzinfo
from email.message import EmailMessage
from email.utils import parseaddr

from finchie_parser.errors import InvalidDocumentError

PDF = "application/pdf"


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
        attachments = []
        for part in self.content.walk():
            if part.is_multipart():
                continue
            name = part.get_filename() or ""
            # Banks send PDFs as application/octet-stream as often as application/pdf.
            if part.get_content_type() != PDF and not name.lower().endswith(".pdf"):
                continue
            data = part.get_payload(decode=True)
            if isinstance(data, bytes) and data.startswith(b"%PDF"):
                attachments.append(Attachment(name or "attachment.pdf", data))
        return attachments


def read_mail(content: bytes) -> Mail:
    if not content:
        raise InvalidDocumentError("document is empty")
    if content.startswith(b"%PDF"):
        raise InvalidDocumentError("a bare PDF: statements are uploaded as their e-mails (ADR 0014)")
    message = email.message_from_bytes(content, policy=email.policy.default)
    if not isinstance(message, EmailMessage) or not message.keys():
        raise InvalidDocumentError("not an e-mail message")
    return unwrap(message)


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


_FORWARD_MARKER = "---------- Forwarded message ---------"
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
    start = text.rfind(_FORWARD_MARKER)
    if start < 0:
        return None
    fields: dict[str, str] = {}
    last = ""
    for line in text[start + len(_FORWARD_MARKER) :].splitlines()[1:]:
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


def is_attachment(part: EmailMessage) -> bool:
    # Python's is_attachment() counts only "Content-Disposition: attachment"; a named part marked inline is a file too.
    return part.is_attachment() or bool(part.get_filename())


def find_body(message: EmailMessage) -> EmailMessage | None:
    """HTML, else plain text, searched for as `EmailMessage.get_body` does, but never a file."""
    candidates = list(body_candidates(message))
    for subtype in ("html", "plain"):
        if body := next((part for part in candidates if part.get_content_subtype() == subtype), None):
            return body
    return None


def body_candidates(part: EmailMessage) -> Iterator[EmailMessage]:
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


def text_of(part: EmailMessage) -> str:
    try:
        return part.get_content()
    except LookupError:
        # A charset Python does not know: most of the text is still readable as UTF-8.
        payload = part.get_payload(decode=True)
        return payload.decode("utf-8", errors="replace") if isinstance(payload, bytes) else ""
