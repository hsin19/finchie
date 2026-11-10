from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, baseline, card

from finchie_parser.model import Kind, Statement
from finchie_parser.ocr import OcrLine
from finchie_parser.parsers import Parsed
from finchie_parser.parsers.hsbc import HsbcParser
from finchie_parser.pdf import Document, Word


@dataclass
class LineOcr:
    """HSBC's descriptions, by the line of the page's text whose row they describe: each placed as OCR boxes
    sit, from above that line's baseline (FakeDocument's) to just below it."""

    descriptions: dict[int, dict[int, str]] = field(default_factory=dict)

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        return [
            OcrLine(text, top=baseline(n) - 10, bottom=baseline(n) + 0.5, left=left + 10)
            for n, text in self.descriptions.get(page_index, {}).items()
        ]


PAGE_1 = """1/2
HSBC Traveller s Infinite Credit Card
2026/05/08 3,000
2026/04/21 / -3,000
280,000 0
12,162"""

# Line numbers matter: LineOcr places descriptions on them.
PAGE_2 = """2/2 O
3,000
04/05 04/05 -3,000
04/10 04/12 DEU EUR -10.00 04/10 -340
-3,340
-340
03/25 03/25 8,000
03/30 03/31 30
03/30 03/31 FRA EUR 60.00 03/30 2,000
04/02 04/03 JPN JPY 10,000.00 04/02 2,100
04/02 04/03 32
12,162
11,822
140,000 28,000"""

OCR = {
    1: {
        2: "全國繳費網繳款",
        3: "RefundTaxFreeShop",
        6: "年費",
        7: "國外交易手續費CAFEDEFLORE",
        8: "CAFEDEFLOREPARIS",
        9: "RAMENSHOPTOKYO",
        10: "國外交易手續費RAMENSHOP",
    }
}


def parse(ocr: LineOcr | None) -> Parsed:
    return HsbcParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=ocr)


def test_hsbc_reads_unlabelled_statement_fields() -> None:
    statement = card(parse(LineOcr(OCR)))
    assert statement.due_date == date(2026, 5, 8)
    assert statement.closing_date == date(2026, 4, 21)
    assert statement.reconciliation.stated == Decimal(12162)
    assert statement.total_due == Decimal(11822)


def test_hsbc_reconciles_new_charges_only() -> None:
    # The payment and the refund belong to the previous balance, not to this period's charges.
    reconciliation = card(parse(LineOcr(OCR))).reconciliation
    assert reconciliation.balanced
    assert reconciliation.computed == Decimal(12162)


def test_hsbc_takes_descriptions_from_ocr_and_classifies_with_them() -> None:
    rows = [(tx.description, tx.kind, tx.amount) for tx in card(parse(LineOcr(OCR))).transactions]
    assert rows == [
        ("全國繳費網繳款", Kind.PAYMENT, Decimal(-3000)),
        ("RefundTaxFreeShop", Kind.REFUND, Decimal(-340)),
        ("年費", Kind.ANNUAL_FEE, Decimal(8000)),
        ("國外交易手續費CAFEDEFLORE", Kind.FOREIGN_TRANSACTION_FEE, Decimal(30)),
        ("CAFEDEFLOREPARIS", Kind.PURCHASE, Decimal(2000)),
        ("RAMENSHOPTOKYO", Kind.PURCHASE, Decimal(2100)),
        ("國外交易手續費RAMENSHOP", Kind.FOREIGN_TRANSACTION_FEE, Decimal(32)),
    ]


def test_hsbc_maps_countries_and_foreign_amounts() -> None:
    by_description = {tx.description: tx for tx in card(parse(LineOcr(OCR))).transactions}
    cafe = by_description["CAFEDEFLOREPARIS"]
    assert (cafe.country, cafe.original_currency, cafe.original_amount) == ("FR", "EUR", Decimal("60.00"))
    assert by_description["RAMENSHOPTOKYO"].country == "JP"
    assert by_description["RefundTaxFreeShop"].country == "DE"


def test_hsbc_without_ocr_still_reconciles_and_warns() -> None:
    parsed = parse(None)
    assert card(parsed).reconciliation.balanced
    assert all(tx.description == "" for tx in card(parsed).transactions)
    assert parsed.warnings


def test_hsbc_warns_about_rows_ocr_missed() -> None:
    partial = {1: {n: text for n, text in OCR[1].items() if n != 9}}
    parsed = parse(LineOcr(partial))
    assert len(parsed.warnings) == 1
    assert card(parsed).reconciliation.balanced


# The payments section lists a payment and the annual fee's waiver; the new charges the annual fee,
# revolving interest, a late fee, a cash-back credit, and a foreign transaction fee.
CHARGES_PAGE = """2/2 O
3,000
04/05 04/05 -3,000
04/07 04/07 -1,800
-4,800
-1,800
03/25 03/25 1,800
04/21 04/21 150
04/21 04/21 300
04/02 04/02 -50
04/02 04/03 32
2,232
432
140,000 28,000"""

CHARGES_OCR = {
    1: {
        2: "全國繳費網繳款",
        3: "年費減免",
        6: "年費",
        7: "循環信用利息",
        8: "違約金",
        9: "現金回饋",
        10: "國外交易手續費CAFEDEFLORE",
    }
}


def parse_charges(ocr: LineOcr | None) -> Statement:
    return card(HsbcParser().parse(FakeDocument([PAGE_1, CHARGES_PAGE]), ocr=ocr))


def test_hsbc_classifies_plain_rows_by_the_words_in_their_description() -> None:
    statement = parse_charges(LineOcr(CHARGES_OCR))
    assert [(tx.description, tx.kind, tx.amount) for tx in statement.transactions] == [
        ("全國繳費網繳款", Kind.PAYMENT, Decimal(-3000)),
        ("年費減免", Kind.ANNUAL_FEE, Decimal(-1800)),
        ("年費", Kind.ANNUAL_FEE, Decimal(1800)),
        ("循環信用利息", Kind.PURCHASE, Decimal(150)),
        ("違約金", Kind.PURCHASE, Decimal(300)),
        ("現金回饋", Kind.REBATE, Decimal(-50)),
        ("國外交易手續費CAFEDEFLORE", Kind.FOREIGN_TRANSACTION_FEE, Decimal(32)),
    ]
    assert statement.reconciliation.balanced
    assert statement.reconciliation.computed == Decimal(2232)


@pytest.mark.parametrize("ocr", [None, LineOcr({1: dict.fromkeys(CHARGES_OCR[1], "#?!")})], ids=["no OCR", "noisy"])
def test_hsbc_without_readable_words_keeps_payments_and_foreign_transaction_fees(ocr: LineOcr | None) -> None:
    statement = parse_charges(ocr)
    assert [tx.kind for tx in statement.transactions] == [Kind.PAYMENT] * 2 + [Kind.FOREIGN_TRANSACTION_FEE] * 5
    assert statement.reconciliation.balanced
    assert statement.reconciliation.computed == Decimal(2232)


# Page 1's summary as positioned words: its own figures at the left, the travel points box at the right,
# both drawn without labels (ADR 0019). Positions as on the real page, in PDF points.
def summary_words(*points: str) -> list[Word]:
    left = [
        Word("2026/04/22", 141.4, 192.7, 230.6),
        Word("2026/04/07", 141.4, 192.7, 243.6),
        Word("1,500", 345.4, 377.0, 269.7),
    ]
    tops = (247.6, 264.4, 281.5, 298.5, 312.7)
    box = [Word(text, 572.6 - 6 * len(text), 572.6, top) for text, top in zip(points, tops, strict=False)]
    return left + box + [Word("(", 456.5, 459.4, 281.5), Word(")", 486.7, 489.6, 281.5)]


def test_hsbc_reads_its_travel_points_box_by_position() -> None:
    document = FakeDocument([PAGE_1, PAGE_2], words={0: summary_words("10,770", "18,077", "0", "28,847", "120")})
    parsed = HsbcParser().parse(document, ocr=None)
    [program] = card(parsed).points
    assert (program.name, program.closing) == ("滙豐旅遊積分", Decimal(28847))
    assert [(line.label, line.amount) for line in program.lines] == [("本期新增積分", Decimal(18077))]
    # Next month's expiring points, by its last day.
    assert [(e.amount, e.by) for e in program.expiries] == [(Decimal(120), date(2026, 5, 31))]
    assert not any("滙豐旅遊積分" in w for w in parsed.warnings)


def test_hsbc_card_without_a_points_box_has_no_points_and_a_broken_box_is_a_warning() -> None:
    assert card(HsbcParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None)).points == []
    broken = FakeDocument([PAGE_1, PAGE_2], words={0: summary_words("10,770", "18,077", "0")})
    parsed = HsbcParser().parse(broken, ocr=None)
    assert card(parsed).points == []
    assert "滙豐旅遊積分: 3 figures in its box, not 5; points not read" in parsed.warnings
