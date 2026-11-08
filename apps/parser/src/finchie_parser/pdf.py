"""Decrypt a PDF and expose what the bank parsers read: text, positioned lines, page images."""

import io
import threading
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from types import TracebackType
from typing import TYPE_CHECKING, Protocol, Self

import pdfplumber
import pikepdf
import pypdfium2
from PIL.Image import Image

from finchie_parser.documents import Attachment
from finchie_parser.errors import InvalidDocumentError, PasswordRequiredError
from finchie_parser.passwords import MissingDetailError, Passwords

if TYPE_CHECKING:
    from finchie_parser.parsers.base import StatementParser

# PDFium is not thread-safe, not even for separate documents, and the server parses on several
# threads at once. Every call into it (open, render, close) holds this lock.
_PDFIUM = threading.Lock()


@dataclass(frozen=True)
class TextLine:
    text: str
    top: float
    bottom: float


@dataclass(frozen=True)
class Word:
    """A word and where it is: tables whose columns can be empty are read by position."""

    text: str
    x0: float
    x1: float
    top: float


class Document(Protocol):
    """What a bank parser may read. Tests provide fakes with synthetic text."""

    filename: str

    def page_texts(self, limit: int | None = None) -> list[str]:
        """The text of every page, or of the first `limit` pages."""
        ...

    def page_lines(self, page_index: int) -> list[TextLine]: ...

    def page_words(self, page_index: int) -> list[Word]: ...

    def render(self, page_index: int, scale: float) -> Image: ...


class PdfDocument:
    def __init__(self, filename: str, data: bytes) -> None:
        self.filename = filename
        self._data = data
        self._plumber = pdfplumber.open(io.BytesIO(data))
        self._pdfium: pypdfium2.PdfDocument | None = None
        # Extracted on demand, in page order: recognizing a statement reads only its first pages.
        self._texts: list[str] = []

    def page_texts(self, limit: int | None = None) -> list[str]:
        pages = self._plumber.pages[:limit]
        for page in pages[len(self._texts) :]:
            self._texts.append(page.extract_text() or "")
        return self._texts[: len(pages)]

    def page_lines(self, page_index: int) -> list[TextLine]:
        lines = self._plumber.pages[page_index].extract_text_lines(return_chars=False)
        return [TextLine(line["text"], line["top"], line["bottom"]) for line in lines]

    def page_words(self, page_index: int) -> list[Word]:
        words = self._plumber.pages[page_index].extract_words()
        return [Word(w["text"], w["x0"], w["x1"], w["top"]) for w in words]

    def render(self, page_index: int, scale: float) -> Image:
        with _PDFIUM:
            if self._pdfium is None:
                self._pdfium = pypdfium2.PdfDocument(self._data)
            page = self._pdfium[page_index]
            bitmap = page.render(scale=scale)
            try:
                # A copy: the PIL image would otherwise share the bitmap's memory, freed below.
                return bitmap.to_pil().copy()
            finally:
                bitmap.close()
                page.close()

    def close(self) -> None:
        self._plumber.close()
        if self._pdfium is not None:
            with _PDFIUM:
                self._pdfium.close()

    def __enter__(self) -> Self:
        return self

    def __exit__(self, *_: type[BaseException] | BaseException | TracebackType | None) -> None:
        self.close()


class PdfSet:
    """One e-mail's PDFs, each decrypted at most once per parser and opened at most once, closed together."""

    def __init__(self, attachments: Sequence[Attachment], passwords: Passwords) -> None:
        self.attachments = list(attachments)
        self._passwords = passwords
        self._decrypted: dict[tuple[int, str], bytes | PasswordRequiredError | InvalidDocumentError] = {}
        self._opened: dict[int, PdfDocument] = {}

    def __len__(self) -> int:
        return len(self.attachments)

    def decrypted(self, index: int, parser: "StatementParser | None") -> bytes:
        """As `decrypt`, with the passwords of the parser that claims the e-mail (ADR 0014); with none but
        the empty one if no parser does. A PDF that fails raises its error again on every later call."""
        key = (index, parser.name if parser else "")
        if key not in self._decrypted:
            attachment = self.attachments[index]
            try:
                if parser is None:
                    self._decrypted[key] = decrypt(attachment, [""], "an e-mail no parser claims")
                else:
                    candidates = self._passwords.candidates(parser.institution, parser.passwords)
                    self._decrypted[key] = decrypt(
                        attachment, candidates, parser.name, f"PARSER_PDF_PASSWORDS__{parser.institution.upper()}"
                    )
            except (PasswordRequiredError, InvalidDocumentError) as error:
                self._decrypted[key] = error
        result = self._decrypted[key]
        if isinstance(result, bytes):
            return result
        raise result

    def open(self, index: int, parser: "StatementParser | None") -> PdfDocument:
        data = self.decrypted(index, parser)
        if index not in self._opened:
            self._opened[index] = PdfDocument(self.attachments[index].filename, data)
        return self._opened[index]

    def close(self) -> None:
        for document in self._opened.values():
            document.close()

    def __enter__(self) -> Self:
        return self

    def __exit__(self, *_: type[BaseException] | BaseException | TracebackType | None) -> None:
        self.close()


def decrypt(attachment: Attachment, passwords: Iterable[str], who: str, chosen_variable: str = "") -> bytes:
    """A copy without encryption, opened with the first of `passwords` that does.

    `passwords` may raise MissingDetailError when it gets to a rule whose detail is not set: that is
    the error to report, since setting the detail is what would open the PDF.
    """
    tried = iter(passwords)
    while True:
        try:
            password = next(tried)
        except StopIteration:
            hint = f"; a password chosen at the bank goes in {chosen_variable}" if chosen_variable else ""
            raise PasswordRequiredError(f"{attachment.filename}: none of {who}'s passwords opens it{hint}") from None
        except MissingDetailError as error:
            raise PasswordRequiredError(f"{attachment.filename}: {who} {error}") from None
        try:
            with pikepdf.open(io.BytesIO(attachment.data), password=password) as pdf:
                decrypted = io.BytesIO()
                pdf.save(decrypted)
        except pikepdf.PasswordError:
            continue
        except pikepdf.PdfError as error:
            raise InvalidDocumentError(f"{attachment.filename}: unreadable PDF ({error})") from error
        return decrypted.getvalue()
