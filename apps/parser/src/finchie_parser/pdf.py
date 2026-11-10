"""Decrypt a PDF and expose what the bank parsers read: text, positioned lines and words, page images.

Every PDF here came from an upload, so what the libraries raise on a malformed one is turned into
InvalidDocumentError, and a PDF longer than MAX_PAGES is refused before anything reads its pages.
"""

import io
import threading
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from functools import partial
from types import TracebackType
from typing import Any, Protocol, Self

import pdfplumber
import pikepdf
import pypdfium2
from pdfplumber.utils.exceptions import MalformedPDFException, PdfminerException
from PIL.Image import Image

from finchie_parser.documents import Attachment
from finchie_parser.errors import InvalidDocumentError

# Far more than any statement prints; past it a PDF is refused, not read.
MAX_PAGES = 200

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

    def page_count(self) -> int: ...

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
        self._plumber = _reading(filename, lambda: pdfplumber.open(io.BytesIO(data)))
        self._pdfium: pypdfium2.PdfDocument | None = None
        # Extracted on demand, in page order: recognizing a statement reads only its first pages.
        self._texts: list[str] = []

    def page_count(self) -> int:
        return len(self._plumber.pages)

    def page_texts(self, limit: int | None = None) -> list[str]:
        pages = self._plumber.pages[:limit]
        for page in pages[len(self._texts) :]:
            self._texts.append(_reading(self.filename, partial(_text, page)))
        return self._texts[: len(pages)]

    def page_lines(self, page_index: int) -> list[TextLine]:
        page = self._plumber.pages[page_index]
        lines = _reading(self.filename, lambda: page.extract_text_lines(return_chars=False))
        return [TextLine(line["text"], line["top"], line["bottom"]) for line in lines]

    def page_words(self, page_index: int) -> list[Word]:
        page = self._plumber.pages[page_index]
        words = _reading(self.filename, lambda: page.extract_words())
        return [Word(w["text"], w["x0"], w["x1"], w["top"]) for w in words]

    def render(self, page_index: int, scale: float) -> Image:
        with _PDFIUM:
            if self._pdfium is None:
                self._pdfium = pypdfium2.PdfDocument(self._data)
            page = self._pdfium[page_index]
            bitmap = page.render(scale=scale)
            try:
                # A copy: the PIL image would otherwise share the bitmap's memory, freed below.
                image: Image = bitmap.to_pil().copy()
                return image
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


def _text(page: Any) -> str:  # a pdfplumber page; pdfplumber ships no type information
    text = page.extract_text()
    return str(text) if text else ""


def _reading[T](filename: str, read: Callable[[], T]) -> T:
    try:
        return read()
    except (PdfminerException, MalformedPDFException) as error:
        raise InvalidDocumentError(f"{filename}: unreadable PDF ({error})") from error


def decrypt(attachment: Attachment, passwords: Iterable[str]) -> bytes | None:
    """A copy without encryption, opened with the first of `passwords` that does, or None if none does.

    InvalidDocumentError for bytes that are not a PDF, or a PDF past MAX_PAGES. `passwords` is consumed
    lazily, and whatever it raises (MissingDetailError) passes through.
    """
    for password in passwords:
        try:
            with pikepdf.open(io.BytesIO(attachment.data), password=password) as pdf:
                if len(pdf.pages) > MAX_PAGES:
                    raise InvalidDocumentError(f"{attachment.filename}: more than {MAX_PAGES} pages")
                decrypted = io.BytesIO()
                pdf.save(decrypted)
        except pikepdf.PasswordError:
            continue
        except pikepdf.PdfError as error:
            raise InvalidDocumentError(f"{attachment.filename}: unreadable PDF ({error})") from error
        return decrypted.getvalue()
    return None
