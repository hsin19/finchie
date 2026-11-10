"""Next Bank's combined statement. Numbers and codes are synthetic (ADR 0006); word positions follow the
bank's layout: a section per account and currency with its balance in the heading, lines dated by month
and day, 收入 before 支出 (printed negative), memos that wrap around their line, and pockets after the
accounts."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, deposit, row

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositKind, DepositStatement
from finchie_parser.parsers.nextbank_deposit import NextBankDepositParser
from finchie_parser.pdf import Word

PAGE_1 = """2026年12月綜合對帳單
帳單期間 2026/12/01~2026/12/31"""
TWD, FX = "123456****7890", "123456****1111"
HEADINGS = (
    ("交易日期", 49, 83),
    ("摘要", 117, 134),
    ("收入", 184, 201),
    ("支出", 245, 262),
    ("餘額", 305, 322),
    ("備註", 434, 451),
)


def heading(top: float, number: str, currency: str, balance: str) -> list[Word]:
    return row(
        top, (f"帳號：{number}", 43, 136), ("｜", 144, 152), (currency, 160, 180), ("｜", 188, 196), (balance, 204, 305)
    )


PAGE_2 = [
    *row(339, ("新臺幣帳戶明細", 52, 139)),
    *heading(369, TWD, "TWD", "新臺幣餘額：NT$10,150"),
    *row(402, *HEADINGS),
    *row(431, ("上期餘額", 109, 143), ("10,000", 303, 336)),
    *row(
        460, ("12/21", 56, 75), ("利息存入", 109, 143), ("300", 201, 217), ("10,300", 303, 336), ("REF0001", 346, 443)
    ),
    *row(
        480,
        ("12/28", 56, 75),
        ("轉帳支出", 109, 143),
        ("-50", 254, 277),
        ("10,250", 303, 336),
        ("玉山銀行-0012345678901;FT0002", 346, 512),
    ),
    # The memo wraps: one row above the line's, one below.
    *row(503, ("INB0001;BPAY", 346, 407)),
    *row(510, ("12/30", 56, 75), ("匯出", 117, 134), ("-100", 254, 277), ("10,150", 303, 336)),
    *row(517, ("GLOBAL;REF0003", 344, 440)),
    *row(540, ("外幣帳戶明細", 52, 127)),
    *heading(570, FX, "USD", "美元餘額：12.50"),
    *row(604, *HEADINGS),
]
# The page breaks after the US$ section's headings.
PAGE_3 = [
    *row(40, *HEADINGS),
    *row(60, ("上期餘額", 109, 143), ("12.00", 301, 336)),
    *row(80, ("12/31", 56, 75), ("利息存入", 109, 143), ("0.50", 201, 217), ("12.50", 301, 336)),
    *heading(110, FX, "JPY", "日圓餘額：1,000"),
    *row(140, *HEADINGS),
    *row(180, ("活存口袋明細", 52, 139), ("新臺幣參考總額：NT$800", 200, 320)),
    *row(200, ("旅行基金", 52, 120)),
    *row(
        220,
        ("序號：AB12C", 52, 120),
        ("｜", 125, 130),
        ("開立日期：2026/01/01", 135, 230),
        ("｜", 235, 240),
        ("餘額：NT$800", 245, 300),
    ),
    *row(240, ("本月無交易紀錄", 52, 130)),
    *row(300, ("注意事項", 52, 90)),
]


def parse(page_3: list[Word] = PAGE_3, page_1: str = PAGE_1) -> DepositStatement:
    document = FakeDocument([page_1], words={1: PAGE_2, 2: page_3})
    statement = deposit(NextBankDepositParser().parse(document, ocr=None))
    return statement


def test_nextbank_deposit_reads_a_section_per_account_and_currency_and_each_pocket() -> None:
    statement = parse()
    assert (statement.period_start, statement.period_end) == (date(2026, 12, 1), date(2026, 12, 31))
    assert [(a.name, a.number, a.currency, a.opening_balance, a.closing_balance) for a in statement.accounts] == [
        ("新臺幣帳戶", TWD, "TWD", Decimal(10000), Decimal(10150)),
        ("外幣帳戶", FX, "USD", Decimal("12.00"), Decimal("12.50")),
        ("外幣帳戶", FX, "JPY", None, Decimal(1000)),
        ("旅行基金", "AB12C", "TWD", None, Decimal(800)),
    ]


def test_nextbank_deposit_reads_income_before_expense_and_marks_interest() -> None:
    lines = parse().accounts[0].lines
    assert [(line.date, line.description, line.amount, line.kind) for line in lines[:2]] == [
        (date(2026, 12, 21), "利息存入", Decimal(300), DepositKind.INTEREST),
        # Printed "-50" under 支出: money out, not a negative expense.
        (date(2026, 12, 28), "轉帳支出", Decimal(-50), DepositKind.OTHER),
    ]


def test_nextbank_deposit_joins_a_wrapped_memo_to_its_line_in_printed_order() -> None:
    assert [line.memo for line in parse().accounts[0].lines] == [
        "REF0001",
        "玉山銀行-0012345678901;FT0002",
        "INB0001;BPAY GLOBAL;REF0003",
    ]


def test_nextbank_deposit_reads_the_other_side_of_a_transfer_from_its_memo() -> None:
    assert [line.counterparty for line in parse().accounts[0].lines] == ["", "808 0012345678901", ""]


def test_nextbank_deposit_keeps_a_bank_it_has_no_code_for_by_name() -> None:
    page_2 = [Word("某某銀行-123456;FT0002", w.x0, w.x1, w.top) if w.text.startswith("玉山銀行") else w for w in PAGE_2]
    document = FakeDocument([PAGE_1], words={1: page_2, 2: PAGE_3})
    statement = deposit(NextBankDepositParser().parse(document, ocr=None))
    assert statement.accounts[0].lines[1].counterparty == "某某銀行 123456"


def test_nextbank_deposit_refuses_a_memo_row_with_no_line_next_to_it() -> None:
    # Under the yen section's headings, which has no lines.
    stray = [*PAGE_3, *row(160, ("STRAY", 346, 400))]
    with pytest.raises(LayoutError, match="no line next to it"):
        parse(stray)


NEW_YEAR = [Word("1/1", w.x0, w.x1, w.top) if w.text == "12/31" else w for w in PAGE_3]


def test_nextbank_deposit_dates_a_month_and_day_within_a_period_across_a_year_end() -> None:
    statement = parse(NEW_YEAR, page_1="帳單期間 2026/12/15~2027/01/14")
    assert statement.accounts[0].lines[0].date == date(2026, 12, 21)
    assert statement.accounts[1].lines[0].date == date(2027, 1, 1)


def test_nextbank_deposit_refuses_a_date_outside_the_period() -> None:
    with pytest.raises(LayoutError, match="outside the statement period"):
        parse(NEW_YEAR)


def test_nextbank_deposit_reconciles_from_the_balance_brought_forward() -> None:
    statement = parse()
    dollars = statement.accounts[1]
    assert dollars.reconciliation is not None
    assert dollars.reconciliation.balanced
    assert statement.balanced


def test_nextbank_deposit_refuses_a_pockets_lines_until_their_layout_is_seen() -> None:
    with_lines = [w for w in PAGE_3 if w.text != "本月無交易紀錄"] + row(240, ("12/05", 56, 75), ("轉入", 109, 143))
    with pytest.raises(LayoutError, match="pocket"):
        parse(with_lines)
