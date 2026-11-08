"""Shin Kong Securities' monthly statement. Names and figures are synthetic (ADR 0006); word positions follow
the broker's layout: trades with a 合計 row on two lines, a prepaid buy's 預繳金 row, and holdings two to a
row, as of a day into the next month."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument

from finchie_parser.errors import LayoutError
from finchie_parser.model import SecuritiesStatement, TradeSide
from finchie_parser.parsers.skis_securities import SkisSecuritiesParser
from finchie_parser.pdf import Word

# The PDF's text writes 月 and 一 in compatibility forms (⽉, ⼀).
PAGE_1 = """新光證券⽉對帳單
www.sk88.com.tw
親愛的 王 O 明 (12**345) 先⽣/⼩姐，您好！
以下是 115 年 07 ⽉的綜合證券⽉對帳單"""


def row(top: float, *words: tuple[str, float, float]) -> list[Word]:
    return [Word(text, x0, x1, top) for text, x0, x1 in words]


HEADINGS = [
    *row(463, ("交易資料明細", 24, 84)),
    *row(
        479,
        ("成交", 30, 44),
        ("融資⾃備款", 285, 320),
        ("融資⾦額/", 335, 369),
        ("融資利息", 382, 410),
        ("券⼿續費", 423, 451),
    ),
    *row(
        483,
        ("證券名稱", 51, 79),
        ("成交股數", 85, 113),
        ("單價", 123, 137),
        ("類別", 152, 166),
        ("價⾦", 183, 197),
        ("⼿續費", 214, 235),
        ("交易稅", 252, 273),
        ("應收付⾦額", 479, 514),
    ),
    *row(
        487,
        ("⽇期", 30, 44),
        ("融券保證⾦", 285, 320),
        ("融券擔保品", 334, 369),
        ("融券利息", 382, 410),
        ("補充保費", 423, 451),
    ),
]


def trade(top: float, when: str, name: str, shares: str, price: str, category: str, *amounts: tuple[str, float, float]):
    return row(
        top, (when, 25, 44), (name, 51, 90), (shares, 96, 113), (price, 120, 137), (category, 152, 166), *amounts
    )


TRADES = [
    # A sale pays tax; a buy does not.
    *trade(
        499,
        "07/03",
        "範例電子",
        "2,000",
        "50.5",
        "集賣",
        ("101,000", 177, 197),
        ("143", 228, 235),
        ("303", 262, 273),
        ("100,554(收)", 481, 514),
    ),
    *trade(
        511,
        "07/10",
        "範例⼀號ETF",
        "1,000",
        "20.00",
        "集買",
        ("20,000", 177, 197),
        ("28", 228, 235),
        ("20,028(付)", 481, 514),
    ),
    # Paid in advance: nothing left to pay.
    *trade(
        535, "07/20", "範例電子", "500", "48.0", "集買", ("24,000", 177, 197), ("34", 228, 235), ("0(付)", 498, 514)
    ),
    *row(
        548,
        ("預繳⾦", 175, 196),
        ("=", 199, 206),
        ("24,034", 209, 234),
        ("抵繳⾦額", 261, 289),
        ("=", 292, 299),
        ("0", 302, 307),
        ("留置款", 352, 373),
        ("=", 376, 383),
        ("0", 386, 390),
        ("借貸還款", 443, 471),
        ("=", 474, 481),
        ("0", 484, 488),
    ),
]
TOTALS = [
    *row(
        607,
        ("合計", 88, 102),
        ("145,000", 173, 197),
        ("205", 224, 235),
        ("303", 262, 273),
        ("0", 406, 410),
        ("0", 447, 451),
        ("80,526(收)", 481, 514),
    ),
    *row(620, ("0", 406, 410), ("0", 447, 451)),
]
HOLDINGS = [
    *row(650, ("115/08/04", 24, 69), ("的庫存資料明細", 72, 142)),
    *row(
        667,
        ("名稱", 24, 38),
        ("普通", 105, 119),
        ("融資", 149, 163),
        ("融券", 193, 207),
        ("借貸擔保品", 215, 250),
        ("證券名稱", 260, 288),
        ("普通", 317, 331),
        ("融資", 361, 375),
        ("融券", 405, 419),
        ("借貸擔保品", 427, 462),
    ),
    *row(680, ("範例⼀號ETF", 24, 76), ("1,000", 103, 119), ("範例電子", 260, 288), ("3,500", 315, 331)),
    *row(691, ("範例航運", 24, 52), ("12,000", 99, 119)),
    *row(724, ("其他說明及宣導事項", 24, 114)),
]
WORDS = [*HEADINGS, *TRADES, *TOTALS, *HOLDINGS]


def parse(words: list[Word] = WORDS) -> SecuritiesStatement:
    statement = SkisSecuritiesParser().parse(FakeDocument([PAGE_1], words={0: words}), ocr=None).statement
    assert isinstance(statement, SecuritiesStatement)
    return statement


def test_skis_reads_the_month_the_account_and_the_holdings_as_of_their_day() -> None:
    statement = parse()
    assert (statement.period_start, statement.period_end) == (date(2026, 7, 1), date(2026, 7, 31))
    assert statement.account_number == "12**345"
    # Into the next month: the day the statement prints, its trades counted.
    assert statement.holdings_date == date(2026, 8, 4)
    assert [(h.name, h.shares) for h in statement.holdings] == [
        ("範例一號ETF", Decimal(1000)),
        ("範例電子", Decimal(3500)),
        ("範例航運", Decimal(12000)),
    ]


def test_skis_reads_trades_with_their_money_and_a_prepaid_buy() -> None:
    trades = parse().trades
    assert [(t.date, t.name, t.side, t.shares, t.price, t.amount, t.fee, t.tax, t.net, t.prepaid) for t in trades] == [
        (
            date(2026, 7, 3),
            "範例電子",
            TradeSide.SELL,
            Decimal(2000),
            Decimal("50.5"),
            Decimal(101000),
            Decimal(143),
            Decimal(303),
            Decimal(100554),
            None,
        ),
        (
            date(2026, 7, 10),
            "範例一號ETF",
            TradeSide.BUY,
            Decimal(1000),
            Decimal("20.00"),
            Decimal(20000),
            Decimal(28),
            Decimal(0),
            Decimal(-20028),
            None,
        ),
        (
            date(2026, 7, 20),
            "範例電子",
            TradeSide.BUY,
            Decimal(500),
            Decimal("48.0"),
            Decimal(24000),
            Decimal(34),
            Decimal(0),
            Decimal(0),
            Decimal(24034),
        ),
    ]


def test_skis_reconciles_the_trades_against_the_totals_column_by_column() -> None:
    assert parse().reconciliation.balanced
    wrong = [Word("146,000", w.x0, w.x1, w.top) if w.text == "145,000" else w for w in WORDS]
    reconciliation = parse(wrong).reconciliation
    assert not reconciliation.balanced
    assert reconciliation.stated_label == "價金"


@pytest.mark.parametrize(
    ("words", "error"),
    [
        # A margin buy: its category says so.
        (
            [Word("資買", w.x0, w.x1, w.top) if w.text == "集買" and w.top == 511 else w for w in WORDS],
            "margin or short",
        ),
        # An amount in a margin column of a trade's second line.
        (
            [*WORDS[: -len(HOLDINGS) - len(TOTALS)], *row(516, ("1,200", 300, 320)), *TOTALS, *HOLDINGS],
            "margin or short",
        ),
        # Shares under 融資 in the holdings.
        ([*WORDS[:-1], *row(702, ("範例銀行", 24, 52), ("2,000", 143, 163)), WORDS[-1]], "margin, short, or pledged"),
        # A trade dated outside the month.
        (
            [Word("08/01", w.x0, w.x1, w.top) if w.text == "07/03" else w for w in WORDS],
            "outside the statement's month",
        ),
    ],
)
def test_skis_refuses_what_it_has_not_seen_rather_than_misreading_it(words: list[Word], error: str) -> None:
    with pytest.raises(LayoutError, match=error):
        parse(words)
