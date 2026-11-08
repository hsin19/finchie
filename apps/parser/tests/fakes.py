"""Test doubles. Fixture text is synthetic: invented merchants, amounts, and card numbers (ADR 0006)."""

import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

from PIL.Image import Image

from finchie_parser.model import Reconciliation, Statement
from finchie_parser.ocr import Ocr, OcrLine
from finchie_parser.parsers import Parsed, StatementParser
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, TextLine

ROW_HEIGHT = 12.0


@dataclass
class FakeDocument:
    pages: list[str]
    filename: str = "fake.pdf"

    def page_texts(self, limit: int | None = None) -> list[str]:
        return self.pages[:limit]

    def page_lines(self, page_index: int) -> list[TextLine]:
        # Like pdfplumber: a thin band at each line's baseline.
        return [
            TextLine(text, baseline(n) - 1, baseline(n)) for n, text in enumerate(self.pages[page_index].splitlines())
        ]

    def render(self, page_index: int, scale: float) -> Image:
        raise AssertionError("fakes are never rendered")


@dataclass
class FakeOcr:
    """Returns given text for given line numbers of a page, positioned like real OCR boxes."""

    descriptions: dict[int, dict[int, str]] = field(default_factory=dict)
    calls: list[int] = field(default_factory=list)

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        self.calls.append(page_index)
        return [
            OcrLine(text, top=baseline(n) - 10, bottom=baseline(n) + 0.5, left=left + 10)
            for n, text in self.descriptions.get(page_index, {}).items()
        ]


def baseline(line_number: int) -> float:
    return 40 + line_number * ROW_HEIGHT


FAKE_STATEMENT = Statement(
    institution="fake",
    closing_date=date(2026, 4, 7),
    due_date=date(2026, 4, 22),
    currency="TWD",
    total_due=Decimal(0),
    transactions=[],
    reconciliation=Reconciliation(Decimal(0), Decimal(0), "total"),
)


class FakeParser(StatementParser):
    """Claims bank.example's statements; its password rule is whatever the test gives it."""

    name = "fake.statement"
    version = "7"
    institution = "fake"
    sender_domains = ("bank.example",)
    subject_pattern = re.compile(r"statement|帳單", re.IGNORECASE)
    filename_pattern = re.compile(r"^FAKE_\d+\.pdf$")
    text_marker = "FAKE BANK"

    def __init__(self, rule: Callable[[Identity], list[str]] = lambda _: ["secret"]) -> None:
        self.rule = rule
        self.parsed: list[str] = []

    def passwords(self, identity: Identity) -> list[str]:
        return self.rule(identity)

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        self.parsed.append(document.filename)
        return Parsed(FAKE_STATEMENT, ["a warning"])
