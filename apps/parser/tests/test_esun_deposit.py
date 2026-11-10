"""E.SUN's combined statement. Fixture text, numbers, and names are synthetic (ADR 0006); word positions
follow the bank's layout, where only a column tells a withdrawal from a deposit."""

from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, deposit, row

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositStatement
from finchie_parser.parsers.esun_deposit import EsunDepositParser
from finchie_parser.pdf import Word

PAGE_1 = """2026年09月 綜 合 對 帳 單
資料參考日：2026/09/30
對帳單期間：2026/09/01－2026/09/30
親愛的 王＊明 您好，"""

PAGE_2 = """存款 資料日期:2026/09/30
項目 銀行帳號 幣別 存款餘額(元)
臺幣活存 1234567***890 TWD 1,200.00
外幣活存 7654321***098 USD 12.34
幣別說明：新臺幣(TWD)、美元(USD)
臺幣活存交易明細"""

HEADER = [
    ("交易日期", 35, 67),
    ("摘要", 118, 134),
    ("提款", 208, 224),
    ("存款", 293, 309),
    ("轉入帳戶/備註", 321, 373),
    ("對方帳號", 406, 438),
    ("餘額", 558, 574),
]


def table(*rows: list[Word]) -> list[Word]:
    return [
        *row(240, ("銀行帳號", 35, 71), ("1234567***890", 80, 145)),
        *row(260, *HEADER),
        *(word for r in rows for word in r),
    ]


# A deposit sits under 存款, a withdrawal under 提款; the balance is always the right-most amount.
ATM_IN = row(
    279,
    ("115/09/03", 35, 73),
    ("ＡＴＭ跨行轉", 86, 134),
    ("500.00", 297, 317),
    ("永*銀行", 333, 361),
    ("807", 388, 399),
    ("0000000000***123", 402, 455),
    ("1,500.00", 553, 574),
)
CARD_OUT = row(
    299,
    ("115/09/10", 35, 72),
    ("玉山卡款扣繳", 86, 134),
    ("300.00", 201, 232),
    ("王*明", 337, 357),
    ("1,200.00", 533, 574),
)


def parse(*rows: list[Word]) -> DepositStatement:
    document = FakeDocument([PAGE_1, PAGE_2], words={1: table(*rows)})
    statement = deposit(EsunDepositParser().parse(document, ocr=None))
    return statement


def test_esun_deposit_reads_the_period_and_every_account_with_its_balance() -> None:
    statement = parse(ATM_IN, CARD_OUT)
    assert (statement.institution, statement.period_start, statement.period_end) == (
        "esun",
        date(2026, 9, 1),
        date(2026, 9, 30),
    )
    assert [(a.name, a.number, a.currency, a.closing_balance) for a in statement.accounts] == [
        ("臺幣活存", "1234567***890", "TWD", Decimal("1200.00")),
        ("外幣活存", "7654321***098", "USD", Decimal("12.34")),
    ]


def test_esun_deposit_tells_withdrawals_from_deposits_by_their_column() -> None:
    lines = parse(ATM_IN, CARD_OUT).accounts[0].lines
    assert [(line.date, line.description, line.amount, line.balance) for line in lines] == [
        (date(2026, 9, 3), "ATM跨行轉", Decimal("500.00"), Decimal("1500.00")),
        (date(2026, 9, 10), "玉山卡款扣繳", Decimal("-300.00"), Decimal("1200.00")),
    ]


def test_esun_deposit_keeps_the_note_and_the_other_sides_account_apart() -> None:
    transfer, card = parse(ATM_IN, CARD_OUT).accounts[0].lines
    assert (transfer.memo, transfer.counterparty) == ("永*銀行", "807 0000000000***123")
    assert (card.memo, card.counterparty) == ("王*明", "")


def test_esun_deposit_reconciles_through_every_running_balance() -> None:
    statement = parse(ATM_IN, CARD_OUT)
    twd, usd = statement.accounts
    assert twd.reconciliation is not None
    assert twd.reconciliation.balanced
    assert usd.lines == []
    assert usd.reconciliation is None
    assert statement.balanced


def test_esun_deposit_flags_lines_that_do_not_reach_the_printed_balance() -> None:
    # The withdrawal dropped: the deposit's running balance no longer leads to the closing balance.
    assert not parse(ATM_IN).balanced


def test_esun_deposit_refuses_lines_of_an_account_it_has_no_balance_for() -> None:
    other = [Word("9999999***999" if w.text == "1234567***890" else w.text, w.x0, w.x1, w.top) for w in table(ATM_IN)]
    with pytest.raises(LayoutError, match="do not list"):
        EsunDepositParser().parse(FakeDocument([PAGE_1, PAGE_2], words={1: other}), ocr=None)


def test_esun_deposit_refuses_a_row_without_its_amount_and_balance() -> None:
    with pytest.raises(LayoutError, match="expected an amount and a balance"):
        parse(row(279, ("115/09/03", 35, 73), ("ＡＴＭ跨行轉", 86, 134), ("1,500.00", 553, 574)))
