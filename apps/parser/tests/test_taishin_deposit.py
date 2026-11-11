"""Taishin's combined statement. Numbers and names are synthetic (ADR 0006); word positions follow the
bank's layout: the account summaries, a foreign currency half a row above its account, and tables of
lines whose headings carry over a page break."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, deposit, deposit_detail, row

from finchie_parser.errors import LayoutError
from finchie_parser.model import Balance, Check, LineKind, Statement
from finchie_parser.parsers.taishin_deposit import TaishinDepositParser
from finchie_parser.pdf import Word

PAGE_1 = """台 新 銀 行 綜 合 對 帳 單 2026年09月
資料期間：2026/09/01~2026/09/30"""

SAVINGS, RICHART, DOLLARS = "123456****7890", "654321****0987", "11111****222"


SUMMARIES = [
    *row(268, ("新臺幣帳戶概要區", 47, 122)),
    *row(287, ("帳號類別", 78, 113), ("帳戶號碼", 163, 198), ("帳戶餘額", 433, 468), ("備註", 509, 526)),
    *row(304, ("新臺幣活存", 50, 93), (SAVINGS, 150, 211), ("$5,000", 458, 486)),
    *row(321, ("新臺幣活存", 50, 93), (RICHART, 150, 211), ("$12,000", 453, 486), ("Richart", 521, 545)),
    *row(339, ("合計", 47, 65), ("$17,000", 451, 484)),
    *row(430, ("外幣帳戶概要區", 47, 113)),
    *row(448, ("帳戶餘額", 404, 438), ("帳戶餘額", 458, 493)),
    *row(454, ("帳號類別", 56, 90), ("帳戶號碼", 116, 151), ("幣別/匯率", 356, 392), ("備註", 529, 546)),
    *row(462, ("(原幣)", 410, 433), ("(新臺幣)", 460, 492)),
    *row(477, ("USD", 366, 382)),
    *row(
        484, ("外幣活存", 56, 90), (DOLLARS, 108, 160), ("$10.50", 425, 445), ("$330", 481, 499), ("Richart", 513, 538)
    ),
    *row(491, ("31.420", 362, 386)),
    *row(508, ("合計", 47, 65), ("$330", 486, 494)),
    # The same account's yen: a summary row of its own.
    *row(523, ("JPY", 366, 382)),
    *row(
        530, ("外幣活存", 56, 90), (DOLLARS, 108, 160), ("$1,000", 420, 445), ("$64", 481, 499), ("Richart", 513, 538)
    ),
    *row(537, ("0.2050", 362, 386)),
]
TWD_HEADINGS = [
    ("帳號", 75, 92),
    ("日期", 135, 152),
    ("摘要", 185, 203),
    ("支票號碼", 222, 257),
    ("支出金額", 272, 306),
    ("存入金額", 331, 366),
    ("帳戶餘額", 396, 430),
    ("備註", 489, 507),
]
FX_HEADINGS = [h if h[0] != "支票號碼" else ("幣別", 231, 248) for h in TWD_HEADINGS]
TWD_LINES = [
    *row(636, ("新臺幣帳戶的往來明細", 49, 142)),
    *row(654, *TWD_HEADINGS),
    # A withdrawal under 支出金額, then a deposit under 存入金額.
    *row(
        671,
        (RICHART, 53, 114),
        ("2026/09/22", 124, 163),
        ("媒體轉帳", 177, 211),
        ("$3,000", 293, 317),
        ("$9,000", 414, 446),
        ("台新卡費", 450, 490),
    ),
    *row(
        688,
        (RICHART, 53, 114),
        ("2026/09/30", 124, 163),
        ("存款息", 181, 207),
        ("$3,000", 353, 377),
        ("$12,000", 414, 446),
    ),
]
# On the next page: the table's headings, then a line printing an amount in both columns.
FX_PAGE = [
    *row(40, ("外幣帳戶的往來明細", 49, 133)),
    *row(58, *FX_HEADINGS),
    *row(
        75,
        (DOLLARS, 57, 109),
        ("2026/09/01", 124, 163),
        ("INTEREST", 175, 213),
        ("USD", 231, 247),
        ("$0.10", 297, 317),
        ("$0.60", 357, 377),
        ("$10.50", 427, 446),
    ),
    *row(
        92,
        (DOLLARS, 57, 109),
        ("2026/09/15", 124, 163),
        ("外幣轉入", 175, 213),
        ("JPY", 231, 247),
        ("$1,000", 357, 377),
        ("$1,000", 427, 446),
    ),
]


def parse(pages: dict[int, list[Word]]) -> Statement:
    document = FakeDocument([PAGE_1], words=pages)
    statement = deposit(TaishinDepositParser().parse(document, ocr=None))
    return statement


def full() -> Statement:
    return parse({1: SUMMARIES + TWD_LINES, 2: FX_PAGE})


def test_taishin_deposit_reads_every_account_from_the_summaries() -> None:
    statement = full()
    assert (statement.period_start, statement.closing_date) == (date(2026, 9, 1), date(2026, 9, 30))
    end = date(2026, 9, 30)
    assert [(a.name, a.number, a.balances) for a in statement.accounts] == [
        ("新臺幣活存", SAVINGS, [Balance("TWD", end, Decimal(5000))]),
        ("新臺幣活存 Richart", RICHART, [Balance("TWD", end, Decimal(12000))]),
        # The original currencies' balances, not their NT$ worth.
        ("外幣活存 Richart", DOLLARS, [Balance("USD", end, Decimal("10.50")), Balance("JPY", end, Decimal(1000))]),
    ]


def test_taishin_deposit_reports_an_account_in_two_currencies_once_with_the_lines_of_both() -> None:
    dollars = full().accounts[2]
    assert [(line.unit, line.description, line.amount) for line in dollars.lines] == [
        ("USD", "INTEREST", Decimal("0.50")),
        ("JPY", "外幣轉入", Decimal(1000)),
    ]


def test_taishin_deposit_tells_withdrawals_from_deposits_and_marks_interest() -> None:
    lines = full().accounts[1].lines
    assert [(line.date, line.description, line.amount, line.kind, deposit_detail(line).memo) for line in lines] == [
        (date(2026, 9, 22), "媒體轉帳", Decimal(-3000), LineKind.OTHER, "台新卡費"),
        (date(2026, 9, 30), "存款息", Decimal(3000), LineKind.INTEREST, ""),
    ]


def test_taishin_deposit_nets_a_line_with_amounts_in_both_columns_across_a_page_break() -> None:
    interest = full().accounts[2].lines[0]
    assert (interest.amount, interest.kind, deposit_detail(interest).balance) == (
        Decimal("0.50"),
        LineKind.INTEREST,
        Decimal("10.50"),
    )


def test_taishin_deposit_checks_each_account_and_currency_and_leaves_one_without_lines_alone() -> None:
    statement = full()
    assert statement.checks == [
        Check(f"新臺幣活存 Richart {RICHART} TWD 帳戶餘額", "TWD", Decimal(12000), Decimal(12000)),
        Check(f"外幣活存 Richart {DOLLARS} USD 帳戶餘額", "USD", Decimal("10.50"), Decimal("10.50")),
        Check(f"外幣活存 Richart {DOLLARS} JPY 帳戶餘額", "JPY", Decimal(1000), Decimal(1000)),
    ]
    assert statement.balanced


def test_taishin_deposit_flags_only_the_currency_whose_lines_do_not_add_up() -> None:
    # The yen balance the summary prints is not where the yen lines lead.
    summaries = [Word("$1,100", w.x0, w.x1, w.top) if (w.text, w.top) == ("$1,000", 530) else w for w in SUMMARIES]
    statement = parse({1: summaries + TWD_LINES, 2: FX_PAGE})
    assert [(c.unit, c.balanced) for c in statement.checks] == [("TWD", True), ("USD", True), ("JPY", False)]
    assert not statement.balanced


def test_taishin_deposit_refuses_lines_of_an_account_the_summaries_do_not_list() -> None:
    stray = [Word(SAVINGS if w.text == RICHART else w.text, w.x0, w.x1, w.top) for w in TWD_LINES]
    summaries = [w for w in SUMMARIES if w.text not in (SAVINGS, "$5,000") or w.top != 304]
    with pytest.raises(LayoutError, match="do not list"):
        parse({1: summaries + stray})


def test_taishin_deposit_refuses_a_foreign_account_without_its_currency() -> None:
    with pytest.raises(LayoutError, match="no single currency"):
        parse({1: [w for w in SUMMARIES if w.text != "USD"]})
