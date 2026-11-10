"""Deposit statements in general: reconciling an account's lines, and the contract (ADR 0013).

Amounts are synthetic (ADR 0006).
"""

from datetime import date
from decimal import Decimal

from finchie.parser.v1 import parser_pb2 as pb
from finchie_parser.convert import to_response
from finchie_parser.model import DepositAccount, DepositKind, DepositLine, DepositStatement, ParseResult
from finchie_parser.parsers.reconcile import reconcile_account

D = Decimal


def line(amount: str, balance: str | None = None, day: int = 1) -> DepositLine:
    return DepositLine(date(2026, 9, day), "ＡＴＭ跨行轉", D(amount), balance=D(balance) if balance else None)


def test_lines_from_the_opening_balance_account_for_the_closing_balance() -> None:
    reconciliation = reconcile_account(D(100), D(120), [line("50"), line("-30")], "存款餘額")
    assert reconciliation is not None
    assert (reconciliation.computed, reconciliation.stated, reconciliation.balanced) == (D(120), D(120), True)


def test_without_an_opening_balance_the_first_running_balance_says_where_it_started() -> None:
    reconciliation = reconcile_account(None, D(120), [line("50", "150"), line("-30", "120")], "存款餘額")
    assert reconciliation is not None
    assert reconciliation.balanced
    assert reconciliation.stated_label == "存款餘額"


def test_a_dropped_line_is_caught_at_the_first_running_balance_that_no_longer_adds_up() -> None:
    lines = [line("50", "150", day=1), line("-30", "100", day=2)]
    reconciliation = reconcile_account(None, D(100), lines, "存款餘額")
    assert reconciliation is not None
    assert not reconciliation.balanced
    assert (reconciliation.computed, reconciliation.stated) == (D(120), D(100))
    assert reconciliation.stated_label == "2026-09-02 ＡＴＭ跨行轉 的餘額"


def test_a_closing_balance_the_lines_do_not_reach_is_unbalanced() -> None:
    reconciliation = reconcile_account(None, D(999), [line("50", "150")], "存款餘額")
    assert reconciliation is not None
    assert not reconciliation.balanced


def test_with_nothing_to_start_from_there_is_nothing_to_reconcile() -> None:
    assert reconcile_account(None, D(5), [], "存款餘額") is None
    assert reconcile_account(None, D(5), [line("5")], "存款餘額") is None


def account(reconciled: bool | None, currency: str = "TWD") -> DepositAccount:
    lines = [line("50", "150")]
    closing = D(150) if reconciled is not False else D(999)
    reconciliation = None if reconciled is None else reconcile_account(None, closing, lines, "存款餘額")
    return DepositAccount("1234567***890", "臺幣活存", currency, closing, lines, reconciliation)


def test_a_statement_is_balanced_when_every_account_that_can_be_is() -> None:
    statement = DepositStatement("esun", date(2026, 9, 1), date(2026, 9, 30), [account(True), account(None, "USD")])
    assert statement.balanced
    assert not DepositStatement("esun", date(2026, 9, 1), date(2026, 9, 30), [account(True), account(False)]).balanced


def test_a_deposit_statement_goes_out_as_one_with_unset_fields_left_out() -> None:
    interest = DepositLine(
        date(2026, 9, 21), "存款息", D("12"), kind=DepositKind.INTEREST, memo="note", counterparty=""
    )
    statement = DepositStatement(
        "taishin",
        date(2026, 9, 1),
        date(2026, 9, 30),
        [DepositAccount("123456****7890", "新臺幣活存", "TWD", D("100.50"), [interest], None, D("88.50"))],
    )
    response = to_response(ParseResult("taishin.deposit", "1", statement))

    assert not response.HasField("statement")
    deposit = response.deposit_statement
    assert (deposit.institution, deposit.period_end.month, deposit.period_end.day) == ("taishin", 9, 30)
    sent = deposit.accounts[0]
    assert (sent.number, sent.name, sent.currency) == ("123456****7890", "新臺幣活存", "TWD")
    assert (sent.opening_balance.value, sent.closing_balance.value) == ("88.50", "100.50")
    assert not sent.HasField("reconciliation")
    (sent_line,) = sent.lines
    assert (sent_line.amount.value, sent_line.kind, sent_line.memo) == ("12", pb.DEPOSIT_LINE_KIND_INTEREST, "note")
    assert not sent_line.HasField("balance")
