"""Cathay's e-passbook (電子存摺). Numbers and names are synthetic (ADR 0006); word positions follow the
passbook's layout: cover page with account number and type, then 22-line passbook pages where each line
has optional remarks half a row above and description and counterparty details half a row below."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositKind, DepositStatement
from finchie_parser.parsers.cathay_deposit import CathayDepositParser
from finchie_parser.pdf import Word

ACCOUNT = "123-45-678901-2"
COVER = f"""開戶日期：2020年01月01日
臺幣存款
帳號 | {ACCOUNT} 類別 | 數位存款帳戶１—１類
銀行代號 | 013"""

PAGE_2_TEXT = f"帳號：{ACCOUNT}"


def row(top: float, *words: tuple[str, float, float]) -> list[Word]:
    return [Word(text, x0, x1, top) for text, x0, x1 in words]


HEADINGS = [
    *row(90.6, ("匯率", 378.6, 396.8), ("備註", 466.8, 485.0)),
    *row(
        97.0,
        ("行次", 55.5, 73.5),
        ("日期", 96.0, 114.0),
        ("提出", 163.6, 181.6),
        ("存入", 240.9, 258.9),
        ("餘額", 318.1, 336.1),
    ),
    *row(104.1, ("說明", 378.6, 396.6), ("交易資訊", 459.1, 495.1)),
]

LINES = [
    # Line 1: card bill auto-debit with remarks above and description below.
    *row(122.6, ("國泰世華卡", 418.5, 457.2), ("信用卡款", 459.1, 490.0)),
    *row(128.4, ("1", 62.5, 67.0), ("2026/05/19", 82.5, 125.0), ("500", 191.3, 204.8), ("9,500", 334.8, 359.3)),
    *row(134.2, ("信用卡款", 372.2, 403.1)),
    # Line 2: interest deposit.
    *row(160.6, ("2", 62.2, 67.2), ("2026/06/20", 82.5, 125.0), ("50", 273.1, 282.1), ("9,550", 334.8, 359.3)),
    *row(166.4, ("存款息", 372.2, 395.4)),
    # Line 3: outgoing transfer with bank code, account number, and merchant name in 交易資訊.
    *row(192.9, ("3", 62.2, 67.2), ("2026/07/16", 82.5, 125.0), ("350", 191.3, 204.8), ("9,200", 334.8, 359.3)),
    *row(198.7, ("網銀轉帳", 372.2, 403.1), ("(013)0000111122223333範＊商店", 418.5, 520.9)),
    # Line 4: incoming interbank transfer with counterparty only.
    *row(225.1, ("4", 62.2, 67.2), ("2026/08/18", 82.5, 125.0), ("10,000", 257.6, 282.1), ("19,200", 334.8, 359.3)),
    *row(230.9, ("跨行轉入", 372.2, 403.1), ("(812)0012345***678901", 418.5, 502.3)),
    # Unused passbook line numbers and page footer.
    *row(257.4, ("5", 62.2, 67.2)),
    *row(740.6, ("20", 60.0, 69.0)),
    *row(744.4, ("1", 534.4, 542.1)),
]


def parse(pages: dict[int, list[Word]], texts: list[str] | None = None) -> DepositStatement:
    document = FakeDocument(texts or [COVER, PAGE_2_TEXT], words=pages)
    statement = CathayDepositParser().parse(document, ocr=None).statement
    assert isinstance(statement, DepositStatement)
    return statement


def test_cathay_deposit_reads_cover_lines_and_counterparties() -> None:
    statement = parse({1: HEADINGS + LINES})
    assert (statement.institution, statement.period_start, statement.period_end) == (
        "cathay",
        date(2026, 5, 19),
        date(2026, 8, 18),
    )
    (account,) = statement.accounts
    assert (account.number, account.name, account.currency) == (ACCOUNT, "數位存款帳戶1—1類", "TWD")
    assert (account.opening_balance, account.closing_balance) == (Decimal(10000), Decimal(19200))
    assert account.reconciliation is not None
    assert account.reconciliation.balanced

    assert [
        (line.date, line.description, line.amount, line.kind, line.memo, line.counterparty, line.balance)
        for line in account.lines
    ] == [
        (date(2026, 5, 19), "信用卡款", Decimal(-500), DepositKind.OTHER, "國泰世華卡 信用卡款", "", Decimal(9500)),
        (date(2026, 6, 20), "存款息", Decimal(50), DepositKind.INTEREST, "", "", Decimal(9550)),
        (
            date(2026, 7, 16),
            "網銀轉帳",
            Decimal(-350),
            DepositKind.OTHER,
            "範*商店",
            "013 0000111122223333",
            Decimal(9200),
        ),
        (
            date(2026, 8, 18),
            "跨行轉入",
            Decimal(10000),
            DepositKind.OTHER,
            "",
            "812 0012345***678901",
            Decimal(19200),
        ),
    ]


def test_cathay_deposit_catches_a_running_balance_that_does_not_add_up() -> None:
    broken = [Word("19,000" if w.text == "19,200" else w.text, w.x0, w.x1, w.top) for w in HEADINGS + LINES]
    (account,) = parse({1: broken}).accounts
    assert account.reconciliation is not None
    assert not account.reconciliation.balanced


def test_cathay_deposit_refuses_an_empty_passbook_or_mismatched_page_account() -> None:
    with pytest.raises(LayoutError, match="no transactions"):
        parse({1: HEADINGS})
    with pytest.raises(LayoutError, match="cover is"):
        parse({1: HEADINGS + LINES}, texts=[COVER, "帳號：999-99-999999-9"])
