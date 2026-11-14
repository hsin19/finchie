"""Tiny generated PDFs: no real documents in the repository (ADR 0006)."""

import email
import email.policy
import io
from email.message import EmailMessage

import pikepdf

PNG = b"\x89PNG\r\n\x1a\n fake image"


def blank_pdf(user_password: str | None = None) -> bytes:
    pdf = pikepdf.new()
    pdf.add_blank_page()
    return _save(pdf, user_password)


def text_pdf(*pages: str, user_password: str | None = None) -> bytes:
    """A page per argument, each a line of ASCII text that pdfplumber reads back."""
    pdf = pikepdf.new()
    font = pikepdf.Dictionary(Type=pikepdf.Name.Font, Subtype=pikepdf.Name.Type1, BaseFont=pikepdf.Name.Helvetica)
    for text in pages:
        page = pdf.add_blank_page()
        page.Resources = pikepdf.Dictionary(Font=pikepdf.Dictionary(F1=font))
        page.Contents = pdf.make_stream(b"BT /F1 12 Tf 72 720 Td " + pikepdf.String(text).unparse() + b" Tj ET")
    return _save(pdf, user_password)


def _save(pdf: pikepdf.Pdf, user_password: str | None) -> bytes:
    buffer = io.BytesIO()
    if user_password is None:
        pdf.save(buffer)
    else:
        pdf.save(buffer, encryption=pikepdf.Encryption(owner="owner-secret", user=user_password, R=6))
    return buffer.getvalue()


def email_with(
    *attachments: tuple[str, str, bytes], sender: str = "statements@bank.example", subject: str = "範例銀行 電子帳單"
) -> bytes:
    """attachments: (filename, content type, data)."""
    message = EmailMessage()
    message["From"] = sender
    message["To"] = "someone@example.com"
    message["Subject"] = subject
    message.set_content("See attachment.")
    for filename, content_type, data in attachments:
        maintype, subtype = content_type.split("/")
        message.add_attachment(data, maintype=maintype, subtype=subtype, filename=filename)
    return message.as_bytes()


def statement_email(
    pdf: bytes, sender: str = "範例銀行 <statements@bank.example>", subject: str = "範例銀行 2026年11月 信用卡電子帳單"
) -> bytes:
    message = EmailMessage()
    message["From"] = sender
    message["To"] = "someone@example.com"
    message["Subject"] = subject
    message["Date"] = "Fri, 06 Nov 2026 09:30:00 +0800"
    message.set_content("Plain version of the body.")
    message.add_alternative('<p>本期帳單</p><img src="cid:logo@bank.example">', subtype="html")
    html = message.get_body(("html",))
    assert html is not None
    html.add_related(PNG, maintype="image", subtype="png", cid="<logo@bank.example>")
    message.add_attachment(pdf, maintype="application", subtype="octet-stream", filename="帳單_11511.pdf")
    message.add_attachment(b"<html>slip</html>", maintype="text", subtype="html", filename="slip.html")
    message.add_attachment(b"signature", maintype="application", subtype="pkcs7-signature", filename="smime.p7s")
    return message.as_bytes()


OWNER = "Owner <owner@mail.example>"

# Gmail's header block for an inline forward, labels as a Chinese-language Gmail writes them.
_GMAIL_BLOCK_ZH = """---------- Forwarded message ---------
寄件者： {sender}
Date: 2026年11月6日週五 下午9:30
Subject: {subject}
To: <someone@example.com>
"""
# The Gmail app in Chinese: the marker in Chinese too, every label, and the owner's signature above it.
_GMAIL_APP_BLOCK_ZH = """Best Regards,
範例

---------- 轉寄的郵件 ---------
寄件者： {sender}
日期：2026年11月6日週五，下午9:30
主旨：{subject}
收件者：<someone@example.com>
"""
_GMAIL_BLOCK_EN = """---------- Forwarded message ---------
From: {sender}
Date: Fri, Nov 6, 2026 at 9:30 PM
Subject: {subject}
To: <someone@example.com>
"""


def gmail_forward(original: bytes, *, english: bool = False, html_only: bool = False, app: bool = False) -> bytes:
    """As Gmail forwards by hand: the owner's headers, the original's in the body, its attachments copied."""
    inner = email.message_from_bytes(original, policy=email.policy.default)
    assert isinstance(inner, EmailMessage)
    template = _GMAIL_APP_BLOCK_ZH if app else _GMAIL_BLOCK_EN if english else _GMAIL_BLOCK_ZH
    block = template.format(sender=inner["From"], subject=inner["Subject"])
    message = EmailMessage()
    message["From"] = OWNER
    message["To"] = "finchie@mailbox.example"
    message["Subject"] = f"Fwd: {inner['Subject']}"
    message["Date"] = "Fri, 06 Nov 2026 21:31:00 +0800"
    # Gmail quotes the original's body below the block, its own forwarded headers included.
    plain = inner.get_body(("plain",))
    quoted = plain.get_content() if plain is not None else ""
    markup = '<div class="gmail_attr">' + block.replace("<", "&lt;").replace(">", "&gt;").replace("\n", "<br>")
    markup += f"</div><div>{quoted.replace(chr(10), '<br>')}</div>"
    if html_only:
        message.set_content(markup, subtype="html")
    else:
        message.set_content(f"\n\n{block}\n\n{quoted}")
        message.add_alternative(markup, subtype="html")
    for part in inner.iter_attachments():
        data = part.get_payload(decode=True)
        assert isinstance(data, bytes)
        maintype, subtype = part.get_content_type().split("/")
        message.add_attachment(data, maintype=maintype, subtype=subtype, filename=part.get_filename())
    return message.as_bytes()


def attached_forward(original: bytes) -> bytes:
    """Forwarded as an attachment, as other mail clients can."""
    inner = email.message_from_bytes(original, policy=email.policy.default)
    message = EmailMessage()
    message["From"] = OWNER
    message["Subject"] = f"Fwd: {inner['Subject']}"
    message.set_content("Forwarded.")
    message.add_attachment(inner)
    return message.as_bytes()
