"""Taipei Fubon's bank statement. Numbers are synthetic (ADR 0006); word positions follow the bank's
layout, where headings are centred over their columns and an account's lines open with the balance
brought forward."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, check, deposit, deposit_detail, row

from finchie_parser.errors import LayoutError
from finchie_parser.model import Balance, Statement
from finchie_parser.parsers.fubon_deposit import FubonDepositParser
from finchie_parser.pdf import Word

PAGE_1 = """對帳單期間：2026/10/01~2026/10/31
帳 戶 總 覽"""
NUMBER = "12345678****90"


def spaced(top: float, text: str) -> list[Word]:
    """A title printed one character at a time, as 資 產 持 有 明 細."""
    return [Word(char, 248 + 17 * i, 261 + 17 * i, top) for i, char in enumerate(text)]


HOLDINGS = [
    *spaced(87, "資產持有明細"),
    *row(117, ("台幣存款", 14, 62)),
    *row(
        134,
        ("帳戶種類", 21, 52),
        ("分行", 74, 90),
        ("帳號", 131, 147),
        ("幣別", 179, 195),
        ("餘額", 281, 297),
        ("備註", 548, 563),
    ),
    *row(145, ("活期存款", 21, 52), ("營業部", 70, 93), (NUMBER, 110, 167), ("TWD", 178, 195), ("16,000.00", 293, 331)),
    *row(157, ("台幣存款總計", 14, 61), ("16,000.00", 288, 331)),
]
LINES = [
    *spaced(201, "當月交易明細"),
    *row(242, ("台幣活存", 14, 62)),
    *row(
        259,
        ("帳號", 47, 62),
        ("日期", 120, 135),
        ("摘要", 178, 193),
        ("支出", 265, 280),
        ("收入", 388, 404),
        ("餘額", 511, 527),
    ),
    *row(270, (NUMBER, 26, 83), ("2026/10/01", 107, 148), ("承轉結餘", 170, 201), ("30,000.00", 543, 581)),
    # Rent out, under 支出, with the account it went to printed past the amounts.
    *row(
        281,
        ("2026/10/01", 107, 148),
        ("行動自轉", 170, 201),
        ("20,000.00", 300, 334),
        ("00000000012345", 398, 458),
        ("10,000.00", 543, 581),
    ),
    # Money in, under 收入.
    *row(292, ("2026/10/15", 107, 148), ("跨行轉入", 170, 201), ("6,000.00", 424, 458), ("16,000.00", 543, 581)),
    *spaced(314, "訊息公告"),
]


def parse(words: list[Word]) -> Statement:
    statement = deposit(FubonDepositParser().parse(FakeDocument([PAGE_1], words={1: words}), ocr=None))
    return statement


def test_fubon_deposit_reads_the_holdings_and_the_balance_brought_forward() -> None:
    statement = parse(HOLDINGS + LINES)
    assert (statement.period_start, statement.closing_date) == (date(2026, 10, 1), date(2026, 10, 31))
    (account,) = statement.accounts
    assert (account.name, account.number) == ("活期存款", NUMBER)
    # The balance brought forward is the one at the end of the day before the period.
    assert account.balances == [
        Balance("TWD", date(2026, 9, 30), Decimal(30000)),
        Balance("TWD", date(2026, 10, 31), Decimal(16000)),
    ]


def test_fubon_deposit_tells_withdrawals_from_deposits_by_the_centred_headings() -> None:
    (account,) = parse(HOLDINGS + LINES).accounts
    details = [(line, deposit_detail(line)) for line in account.lines]
    assert [(line.description, line.amount, d.counterparty, d.balance) for line, d in details] == [
        ("行動自轉", Decimal(-20000), "00000000012345", Decimal(10000)),
        ("跨行轉入", Decimal(6000), "", Decimal(16000)),
    ]


def test_fubon_deposit_checks_the_lines_from_the_printed_opening_balance() -> None:
    name = f"活期存款 {NUMBER} TWD 餘額"
    assert check(parse(HOLDINGS + LINES), name).balanced
    off = [Word("31,000.00", w.x0, w.x1, w.top) if w.text == "30,000.00" else w for w in LINES]
    statement = parse(HOLDINGS + off)
    # From the opening balance, the first line's running balance is the first that no longer adds up.
    (only,) = statement.checks
    assert (only.name, only.computed, only.stated) == (
        f"活期存款 {NUMBER} TWD 2026-10-01 行動自轉 的餘額",
        Decimal(11000),
        Decimal(10000),
    )
    assert not statement.balanced


def test_fubon_deposit_refuses_lines_of_an_account_the_holdings_do_not_list() -> None:
    with pytest.raises(LayoutError, match="do not list"):
        parse([w for w in HOLDINGS if w.top != 145] + LINES)
