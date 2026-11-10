"""Test doubles and fixture builders. Fixture text is synthetic: invented merchants, amounts, and card numbers
(ADR 0006).

The fakes are checked against the protocols the parsers read (`Document`, `Ocr`) by the type checker in
`task parser:lint` (`_conforms` below), and FakeDocument against PdfDocument in test_pdf.py.
"""

import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

from PIL.Image import Image

from finchie_parser.model import (
    AccountKind,
    AccountReport,
    CardLine,
    Check,
    DepositLine,
    Line,
    Statement,
    Trade,
)
from finchie_parser.ocr import Ocr, OcrLine
from finchie_parser.parsers import Parsed, StatementParser
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, TextLine, Word

ROW_HEIGHT = 12.0


def row(top: float, *words: tuple[str, float, float]) -> list[Word]:
    """One printed row of positioned words: (text, x0, x1) each, all at `top`."""
    return [Word(text, x0, x1, top) for text, x0, x1 in words]


def baseline(line_number: int) -> float:
    """Where FakeDocument puts a page's text line `line_number`, as pdfplumber reports a line's bottom."""
    return 40 + line_number * ROW_HEIGHT


@dataclass
class FakeDocument:
    """A PDF as a parser reads it: each page's text and, for parsers that read tables by position, its words.
    Pages past `pages` that have words have no text."""

    pages: list[str]
    filename: str = "fake.pdf"
    words: dict[int, list[Word]] = field(default_factory=dict)

    def page_count(self) -> int:
        return max(len(self.pages), max(self.words, default=-1) + 1)

    def page_texts(self, limit: int | None = None) -> list[str]:
        texts = [self.pages[i] if i < len(self.pages) else "" for i in range(self.page_count())]
        return texts[:limit]

    def page_lines(self, page_index: int) -> list[TextLine]:
        # Like pdfplumber: a thin band at each line's baseline.
        text = self.page_texts()[page_index]
        return [TextLine(line, baseline(n) - 1, baseline(n)) for n, line in enumerate(text.splitlines())]

    def page_words(self, page_index: int) -> list[Word]:
        return self.words.get(page_index, [])

    def render(self, page_index: int, scale: float) -> Image:
        raise AssertionError("fakes are never rendered")


@dataclass
class FakeOcr:
    """Returns the given OCR lines of a page, whatever strip of it is asked for."""

    pages: dict[int, list[OcrLine]] = field(default_factory=dict)

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        return self.pages.get(page_index, [])


def card(parsed: Parsed) -> Statement:
    """The card bill a parser read; fails the test if it read another kind of statement."""
    statement = parsed.statement
    assert statement.card_bill is not None, "not a card bill"
    assert statement.period_start is None
    return statement


def deposit(parsed: Parsed) -> Statement:
    """The deposit statement a parser read: deposit accounts only, each reported once."""
    statement = parsed.statement
    assert statement.card_bill is None, "a card bill"
    assert {a.kind for a in statement.accounts} == {AccountKind.DEPOSIT}
    numbers = [a.number for a in statement.accounts]
    assert len(numbers) == len(set(numbers)), f"an account reported twice: {numbers}"
    return statement


def securities(parsed: Parsed) -> Statement:
    statement = parsed.statement
    assert statement.card_bill is None, "a card bill"
    assert [a.kind for a in statement.accounts] == [AccountKind.SECURITIES]
    return statement


def account(statement: Statement, kind: AccountKind) -> AccountReport:
    """The statement's one report of an account of `kind`."""
    [found] = [a for a in statement.accounts if a.kind is kind]
    return found


def card_lines(statement: Statement) -> list[Line]:
    """A card bill's lines: its card account's."""
    return account(statement, AccountKind.CREDIT_CARD).lines


def card_detail(line: Line) -> CardLine:
    assert isinstance(line.detail, CardLine), line
    return line.detail


def deposit_detail(line: Line) -> DepositLine:
    assert isinstance(line.detail, DepositLine), line
    return line.detail


def trade_detail(line: Line) -> Trade:
    assert isinstance(line.detail, Trade), line
    return line.detail


def check(statement: Statement, name: str) -> Check:
    """The statement's one check named `name`."""
    [found] = [c for c in statement.checks if c.name == name]
    return found


def _conforms(document: FakeDocument, ocr: FakeOcr) -> tuple[Document, Ocr]:
    """Fails the type check when a fake stops matching the protocol it stands in for."""
    return document, ocr


FAKE_STATEMENT = Statement(
    institution="fake",
    period_start=None,
    closing_date=date(2026, 4, 7),
    accounts=[AccountReport(AccountKind.CREDIT_CARD, number="", name="")],
    checks=[Check("total", "TWD", Decimal(0), Decimal(0))],
)


class FakeParser(StatementParser):
    """Claims bank.example's statements; its password rule is whatever the test gives it."""

    name = "fake.statement"
    version = "7"
    institution = "fake"
    sender_domains = ("bank.example",)
    subject_terms = ("帳單",)
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
