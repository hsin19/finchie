import threading
import time

import pypdfium2
import pytest
from fakes import FakeParser
from pdfs import blank_pdf, text_pdf
from PIL import Image

from finchie_parser.documents import Attachment
from finchie_parser.errors import InvalidDocumentError, PasswordRequiredError
from finchie_parser.passwords import Passwords
from finchie_parser.pdf import PdfSet


def one_pdf(data: bytes) -> PdfSet:
    return PdfSet([Attachment("a.pdf", data)], Passwords())


def parser(*rule: str) -> FakeParser:
    return FakeParser(lambda _: list(rule))


def test_unencrypted_pdf_opens_without_passwords() -> None:
    with one_pdf(blank_pdf()) as pdfs:
        assert pdfs.open(0, parser()).page_texts() == [""]
        assert pdfs.open(0, None).page_texts() == [""]


def test_encrypted_pdf_opens_with_any_password_of_its_parser() -> None:
    with one_pdf(blank_pdf("right")) as pdfs:
        assert len(pdfs.open(0, parser("wrong", "right")).page_texts()) == 1


def test_an_encrypted_pdf_no_parser_claims_stays_locked() -> None:
    with one_pdf(blank_pdf("right")) as pdfs, pytest.raises(PasswordRequiredError):
        pdfs.open(0, None)


@pytest.mark.parametrize(
    ("data", "error"),
    [(blank_pdf("right"), PasswordRequiredError), (b"%PDF-1.7 truncated", InvalidDocumentError)],
    ids=["locked", "corrupt"],
)
def test_a_pdf_that_cannot_be_opened_says_why_every_time(data: bytes, error: type[Exception]) -> None:
    with one_pdf(data) as pdfs:
        for _ in range(2):
            with pytest.raises(error):
                pdfs.open(0, parser("wrong"))
            with pytest.raises(error):
                pdfs.decrypted(0, parser("wrong"))


def test_page_texts_reads_every_page_or_the_first_few() -> None:
    with one_pdf(text_pdf("first", "second", "third")) as pdfs:
        document = pdfs.open(0, None)
        assert document.page_texts(1) == ["first"]
        assert document.page_texts() == ["first", "second", "third"]
        assert document.page_texts(2) == ["first", "second"]


class _Concurrency:
    """Counts callers inside fake PDFium at the same time."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.active = 0
        self.peak = 0

    def enter(self) -> None:
        with self._lock:
            self.active += 1
            self.peak = max(self.peak, self.active)
        time.sleep(0.02)
        with self._lock:
            self.active -= 1


def test_pdfium_is_never_called_from_two_threads_at_once(monkeypatch: pytest.MonkeyPatch) -> None:
    calls = _Concurrency()

    class FakeBitmap:
        def to_pil(self) -> Image.Image:
            calls.enter()
            return Image.new("RGB", (1, 1))

        def close(self) -> None:
            calls.enter()

    class FakePage:
        def render(self, scale: float) -> FakeBitmap:
            calls.enter()
            return FakeBitmap()

        def close(self) -> None:
            calls.enter()

    class FakePdfium:
        def __init__(self, data: bytes) -> None:
            calls.enter()

        def __getitem__(self, index: int) -> FakePage:
            calls.enter()
            return FakePage()

        def close(self) -> None:
            calls.enter()

    monkeypatch.setattr(pypdfium2, "PdfDocument", FakePdfium)
    start = threading.Barrier(4)

    def parse_one() -> None:
        with one_pdf(blank_pdf()) as pdfs:
            document = pdfs.open(0, None)
            start.wait()
            document.render(0, 1.0)

    threads = [threading.Thread(target=parse_one) for _ in range(4)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert calls.peak == 1
