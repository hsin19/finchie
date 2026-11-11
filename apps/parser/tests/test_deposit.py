"""Deposit statements in general (ADR 0013, ADR 0021): each account reported once with all its currencies, its
balances on their days, and each currency checked through its lines against its closing balance.

Amounts are synthetic (ADR 0006).
"""

from datetime import date
from decimal import Decimal

import pytest

from finchie_parser.errors import LayoutError
from finchie_parser.model import AccountKind, Balance, Check, DepositLine, LineKind
from finchie_parser.parsers.deposit import Account, Accounts, Entry, reconcile_account

D = Decimal
START, END = date(2026, 9, 1), date(2026, 9, 30)


def entry(amount: str, balance: str | None = None, day: int = 1) -> Entry:
    return Entry(date(2026, 9, day), "ＡＴＭ跨行轉", D(amount), balance=D(balance) if balance else None)


def account(closing: int, lines: list[Entry], opening: int | None = None, currency: str = "TWD") -> Account:
    return Account(
        "1234567***890", currency, "臺幣活存", D(closing), D(opening) if opening is not None else None, lines
    )


def test_lines_from_the_opening_balance_account_for_the_closing_balance() -> None:
    check = reconcile_account(account(120, [entry("50"), entry("-30")], opening=100), "存款餘額")
    assert check == Check("臺幣活存 1234567***890 TWD 存款餘額", "TWD", D(120), D(120))
    assert check.balanced


def test_without_an_opening_balance_the_first_running_balance_says_where_it_started() -> None:
    check = reconcile_account(account(120, [entry("50", "150"), entry("-30", "120")]), "存款餘額")
    assert check is not None
    assert check.balanced


def test_a_dropped_line_is_caught_at_the_first_running_balance_that_no_longer_adds_up() -> None:
    lines = [entry("50", "150", day=1), entry("-30", "100", day=2)]
    check = reconcile_account(account(100, lines), "存款餘額")
    assert check is not None
    assert not check.balanced
    assert (check.computed, check.stated) == (D(120), D(100))
    assert check.name == "臺幣活存 1234567***890 TWD 2026-09-02 ＡＴＭ跨行轉 的餘額"


def test_a_closing_balance_the_lines_do_not_reach_is_unbalanced() -> None:
    check = reconcile_account(account(999, [entry("50", "150")]), "存款餘額")
    assert check is not None
    assert not check.balanced


def test_with_nothing_to_start_from_there_is_nothing_to_check() -> None:
    assert reconcile_account(account(5, []), "存款餘額") is None
    assert reconcile_account(account(5, [entry("5")]), "存款餘額") is None


def two_currencies() -> Accounts:
    accounts = Accounts()
    twd = accounts.add("1234567***890", "TWD", "活期存款", D(150))
    twd.lines = [entry("50", "150", day=3)]
    usd = accounts.add("1234567***890", "USD", "活期存款", D("10.25"))
    usd.opening = D("10.00")
    usd.lines = [Entry(date(2026, 9, 2), "INTEREST", D("0.25"), LineKind.INTEREST, memo="m", counterparty="c")]
    accounts.add("9999999***001", "TWD", "數位帳戶", D(0))
    return accounts


def test_an_account_in_two_currencies_is_one_report_with_both() -> None:
    statement = two_currencies().statement("esun", START, END, "存款餘額")
    assert (statement.period_start, statement.closing_date, statement.card_bill) == (START, END, None)
    first, second = statement.accounts
    assert (first.kind, first.number, first.name) == (AccountKind.DEPOSIT, "1234567***890", "活期存款")
    assert [(line.unit, line.amount, line.kind) for line in first.lines] == [
        ("TWD", D(50), LineKind.OTHER),
        ("USD", D("0.25"), LineKind.INTEREST),
    ]
    assert (second.number, second.balances, second.lines) == ("9999999***001", [Balance("TWD", END, D(0))], [])


def test_a_printed_opening_balance_is_a_balance_on_the_day_before_the_period() -> None:
    [first, _] = two_currencies().statement("esun", START, END, "存款餘額").accounts
    assert first.balances == [
        Balance("TWD", END, D(150)),
        Balance("USD", date(2026, 8, 31), D("10.00")),
        Balance("USD", END, D("10.25")),
    ]


def test_each_currency_of_each_account_is_a_check_of_its_own() -> None:
    statement = two_currencies().statement("esun", START, END, "存款餘額")
    assert [(c.name, c.unit, c.balanced) for c in statement.checks] == [
        ("活期存款 1234567***890 TWD 存款餘額", "TWD", True),
        ("活期存款 1234567***890 USD 存款餘額", "USD", True),
    ]
    assert statement.balanced


def test_a_statement_with_one_currency_that_does_not_add_up_is_not_balanced() -> None:
    accounts = two_currencies()
    usd = accounts.get("1234567***890", "USD")
    assert usd is not None
    usd.closing = D(99)
    statement = accounts.statement("esun", START, END, "存款餘額")
    assert [c.balanced for c in statement.checks] == [True, False]
    assert not statement.balanced


def test_a_line_keeps_its_memo_counterparty_and_running_balance() -> None:
    [first, _] = two_currencies().statement("esun", START, END, "存款餘額").accounts
    twd, usd = first.lines
    assert (twd.date, twd.description) == (date(2026, 9, 3), "ＡＴＭ跨行轉")
    assert twd.detail == DepositLine(memo="", counterparty="", balance=D(150))
    assert usd.detail == DepositLine(memo="m", counterparty="c", balance=None)


def test_one_account_under_two_names_is_a_layout_error() -> None:
    accounts = Accounts()
    accounts.add("1234567***890", "TWD", "活期存款", D(1))
    accounts.add("1234567***890", "USD", "外幣活存", D(1))
    with pytest.raises(LayoutError):
        accounts.statement("esun", START, END, "存款餘額")


def test_a_statement_without_accounts_is_a_layout_error() -> None:
    with pytest.raises(LayoutError):
        Accounts().statement("esun", START, END, "存款餘額")
