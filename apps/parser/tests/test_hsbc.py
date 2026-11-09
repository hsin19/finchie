from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, FakeOcr

from finchie_parser.model import Kind
from finchie_parser.parsers.hsbc import HsbcParser
from finchie_parser.pdf import Word

PAGE_1 = """1/2
HSBC Traveller s Infinite Credit Card
2026/05/08 3,000
2026/04/21 / -3,000
280,000 0
12,162"""

# Line numbers matter: FakeOcr places descriptions on them.
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


def parse(ocr: FakeOcr | None):
    return HsbcParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=ocr)


def test_hsbc_reads_unlabelled_statement_fields() -> None:
    statement = parse(FakeOcr(OCR)).statement
    assert statement.due_date == date(2026, 5, 8)
    assert statement.closing_date == date(2026, 4, 21)
    assert statement.reconciliation.stated == Decimal(12162)
    assert statement.total_due == Decimal(11822)


def test_hsbc_reconciles_new_charges_only() -> None:
    # The payment and the refund belong to the previous balance, not to this period's charges.
    reconciliation = parse(FakeOcr(OCR)).statement.reconciliation
    assert reconciliation.balanced
    assert reconciliation.computed == Decimal(12162)


def test_hsbc_takes_descriptions_from_ocr_and_classifies_with_them() -> None:
    rows = [(tx.description, tx.kind, tx.amount) for tx in parse(FakeOcr(OCR)).statement.transactions]
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
    by_description = {tx.description: tx for tx in parse(FakeOcr(OCR)).statement.transactions}
    cafe = by_description["CAFEDEFLOREPARIS"]
    assert (cafe.country, cafe.original_currency, cafe.original_amount) == ("FR", "EUR", Decimal("60.00"))
    assert by_description["RAMENSHOPTOKYO"].country == "JP"
    assert by_description["RefundTaxFreeShop"].country == "DE"


def test_hsbc_without_ocr_still_reconciles_and_warns() -> None:
    parsed = parse(None)
    assert parsed.statement.reconciliation.balanced
    assert all(tx.description == "" for tx in parsed.statement.transactions)
    assert parsed.warnings


def test_hsbc_warns_about_rows_ocr_missed() -> None:
    partial = {1: {n: text for n, text in OCR[1].items() if n != 9}}
    parsed = parse(FakeOcr(partial))
    assert len(parsed.warnings) == 1
    assert parsed.statement.reconciliation.balanced


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


def parse_charges(ocr: FakeOcr | None):
    return HsbcParser().parse(FakeDocument([PAGE_1, CHARGES_PAGE]), ocr=ocr).statement


def test_hsbc_classifies_plain_rows_by_the_words_in_their_description() -> None:
    statement = parse_charges(FakeOcr(CHARGES_OCR))
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


@pytest.mark.parametrize("ocr", [None, FakeOcr({1: dict.fromkeys(CHARGES_OCR[1], "#?!")})], ids=["no OCR", "noisy"])
def test_hsbc_without_readable_words_keeps_payments_and_foreign_transaction_fees(ocr: FakeOcr | None) -> None:
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
    [program] = parsed.statement.points
    assert (program.name, program.closing) == ("滙豐旅遊積分", Decimal(28847))
    assert [(line.label, line.amount) for line in program.lines] == [("本期新增積分", Decimal(18077))]
    # Next month's expiring points, by its last day.
    assert [(e.amount, e.by) for e in program.expiries] == [(Decimal(120), date(2026, 5, 31))]
    assert not any("滙豐旅遊積分" in w for w in parsed.warnings)


def test_hsbc_card_without_a_points_box_has_no_points_and_a_broken_box_is_a_warning() -> None:
    assert HsbcParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None).statement.points == []
    broken = FakeDocument([PAGE_1, PAGE_2], words={0: summary_words("10,770", "18,077", "0")})
    parsed = HsbcParser().parse(broken, ocr=None)
    assert parsed.statement.points == []
    assert "滙豐旅遊積分: 3 figures in its box, not 5; points not read" in parsed.warnings
