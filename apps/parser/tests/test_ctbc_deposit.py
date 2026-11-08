"""CTBC's bank statement. Numbers are synthetic (ADR 0006). Its text layer spells only digits, so the
fixtures are two layers, as the bank's PDF reads: positioned digits, and the Chinese an OCR reads beside
them, its boxes about 3 points above the text-layer row they share."""

from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositKind, DepositStatement
from finchie_parser.ocr import OcrLine
from finchie_parser.parsers.ctbc_deposit import CtbcDepositParser
from finchie_parser.pdf import Document, Word

PAGE_1 = """P 1 2
2026/09/13 2026/10/12
https://www.ctbcbank.com/twrbo/zh_tw/index.html"""
SAVINGS, DIGITAL, DOLLARS = "11111**2222*", "33333**4444*", "55555**6666*"


@dataclass
class PageOcr:
    """Returns the given lines for a page, whatever strip is asked for."""

    pages: dict[int, list[OcrLine]] = field(default_factory=dict)

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        return self.pages.get(page_index, [])


def said(top: float, *labels: tuple[str, float]) -> list[OcrLine]:
    """OCR boxes of a row whose text-layer words have this top."""
    return [OcrLine(text, top - 9, top + 3, left) for text, left in labels]


def words(top: float, *items: tuple[str, float, float]) -> list[Word]:
    return [Word(text, x0, x1, top) for text, x0, x1 in items]


TEXT = [
    *words(199, (SAVINGS, 133, 181), ("26", 283, 292)),
    *words(210, (DIGITAL, 133, 181), ("15,124", 268, 292)),
    *words(298, (DOLLARS, 133, 181), ("0.00", 277, 292)),
    *words(429, (DIGITAL, 40, 88), ("15,115", 512, 536)),
    *words(440, ("2026/09/21", 107, 146), ("9", 438, 442), ("15,124", 512, 536)),
    *words(450, ("0", 354, 359), ("1", 426, 430)),
    *words(464, ("0", 367, 371), ("9", 438, 442), ("15,124", 512, 536)),
]
OCR = [
    *said(170, ("台幣存款帳戶", 32)),
    *said(186, ("帳戶種類", 35), ("帳號", 160), ("帳戶餘額", 253)),
    *said(199, ("活儲有摺", 34)),
    *said(210, ("台幣數二", 34)),
    *said(268, ("外幣存款帳戶", 32)),
    # 幣別 merged with its neighbour, as OCR sometimes reads it: the currency is found by name.
    *said(285, ("帳戶種類", 35), ("帳號幣別", 160), ("帳戶餘額", 253)),
    *said(298, ("外幣活期", 35), ("美元", 190)),
    *said(400, ("台幣無摺帳戶交易明細", 33)),
    *said(416, ("帳號", 34), ("日期", 126), ("摘要", 264), ("支出", 352), ("存入", 423), ("帳戶餘額", 501)),
    *said(429, ("上期結餘", 249)),
    *said(440, ("利息", 264)),
    *said(450, ("本次合計", 248), ("支出0筆", 331), ("存入1筆", 402)),
    *said(464, ("本期結餘", 248)),
    *said(500, ("優惠權益與服務", 256)),
]


def parse(text: list[Word] = TEXT, ocr: list[OcrLine] = OCR) -> DepositStatement:
    document = FakeDocument([PAGE_1, ""], words={1: text})
    statement = CtbcDepositParser().parse(document, ocr=PageOcr({1: ocr})).statement
    assert isinstance(statement, DepositStatement)
    return statement


def test_ctbc_deposit_reads_the_holdings_with_their_names_and_currencies_from_ocr() -> None:
    statement = parse()
    assert (statement.period_start, statement.period_end) == (date(2026, 9, 13), date(2026, 10, 12))
    assert [(a.name, a.number, a.currency, a.closing_balance) for a in statement.accounts] == [
        ("活儲有摺", SAVINGS, "TWD", Decimal(26)),
        ("台幣數二", DIGITAL, "TWD", Decimal(15124)),
        ("外幣活期", DOLLARS, "USD", Decimal("0.00")),
    ]


def test_ctbc_deposit_reads_a_block_of_lines_from_the_balance_brought_forward_to_the_closing_one() -> None:
    digital = parse().accounts[1]
    assert digital.opening_balance == Decimal(15115)
    assert [(line.date, line.description, line.amount, line.kind, line.balance) for line in digital.lines] == [
        (date(2026, 9, 21), "利息", Decimal(9), DepositKind.INTEREST, Decimal(15124)),
    ]
    assert digital.reconciliation is not None
    assert digital.reconciliation.balanced


def test_ctbc_deposit_needs_ocr() -> None:
    with pytest.raises(LayoutError, match="need OCR"):
        CtbcDepositParser().parse(FakeDocument([PAGE_1, ""], words={1: TEXT}), ocr=None)


def test_ctbc_deposit_refuses_a_currency_it_does_not_know() -> None:
    ocr = [OcrLine("瑞典克朗", line.top, line.bottom, line.left) if line.text == "美元" else line for line in OCR]
    with pytest.raises(LayoutError, match="no known currency"):
        parse(ocr=ocr)


def test_ctbc_deposit_refuses_a_closing_balance_that_differs_from_the_holding() -> None:
    text = [Word("15,125", w.x0, w.x1, w.top) if (w.text, w.top) == ("15,124", 464) else w for w in TEXT]
    with pytest.raises(LayoutError, match="differs from its holding"):
        parse(text=text)
