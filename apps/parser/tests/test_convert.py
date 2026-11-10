"""The model as the proto carries it: every enum value has its proto value, and every field reaches the wire.
Amounts are synthetic (ADR 0006)."""

from dataclasses import replace
from datetime import date
from decimal import Decimal
from enum import Enum

import pytest
from google.protobuf.message import Message

from finchie.parser.v2 import parser_pb2 as pb
from finchie_parser import convert
from finchie_parser.errors import LayoutError
from finchie_parser.model import (
    AccountKind,
    AccountReport,
    Balance,
    CardBill,
    CardLine,
    Check,
    DepositLine,
    Expiry,
    Line,
    LineKind,
    ParseResult,
    Statement,
    Trade,
)

D = Decimal
DAY = date(2026, 10, 21)

CARD_LINE = Line(
    date(2026, 10, 8),
    "TWD",
    D(28009),
    LineKind.PURCHASE,
    "SHOP",
    CardLine(
        posting_date=date(2026, 10, 9),
        original_amount=D("780.00"),
        original_currency="EUR",
        country="IT",
        card_last4="1234",
    ),
)
DEPOSIT_LINE = Line(
    date(2026, 9, 21),
    "USD",
    D("12.50"),
    LineKind.INTEREST,
    "存款息",
    DepositLine(memo="note", counterparty="808 0012345", balance=D("100.50")),
)
TRADE = Trade(
    price=D("1000.5"),
    category="現賣",
    amount=D(1000500),
    fee=D(1425),
    tax=D(3001),
    net=D(996074),
    prepaid=D(1),
)
TRADE_LINE = Line(date(2026, 7, 3), "台積電", D(-1000), LineKind.SELL, "台積電", TRADE)
POINTS_LINE = Line(DAY, "小樹點(信用卡)", D(-1), LineKind.REDEEM, "本期兌/轉換")


def report(kind: AccountKind, *lines: Line) -> AccountReport:
    """A report with every field set, to something other than its default."""
    return AccountReport(
        kind=kind,
        number="1234****5678",
        name="帳戶",
        balances=[Balance("TWD", DAY, D("100.50"))],
        lines=list(lines),
        expiries=[Expiry("小樹點(信用卡)", D(88), date(2026, 4, 30))],
    )


# Every field set, to something other than its default; every kind of line detail once.
FULL = Statement(
    institution="hsbc",
    period_start=date(2026, 9, 1),
    closing_date=DAY,
    accounts=[
        report(AccountKind.CREDIT_CARD, CARD_LINE),
        report(AccountKind.DEPOSIT, DEPOSIT_LINE),
        report(AccountKind.SECURITIES, TRADE_LINE),
        report(AccountKind.POINTS, POINTS_LINE),
    ],
    checks=[Check("本期新增款項", "TWD", D(10), D(10))],
    card_bill=CardBill(date(2026, 11, 8), "TWD", D(95730)),
)


def unset_fields(message: Message, path: str = "") -> list[str]:
    """Every field of `message` left at its default, through nested and repeated messages. Of a oneof, only
    the member that is set counts, if any; the test asks for each member elsewhere."""
    unset = []
    present = {field.name: value for field, value in message.ListFields()}
    for field in message.DESCRIPTOR.fields:
        name = f"{path}{field.name}"
        oneof = field.containing_oneof
        if oneof is not None and message.WhichOneof(oneof.name) != field.name:
            continue
        if field.name not in present:
            unset.append(name)
        elif field.message_type is not None:
            values = present[field.name] if field.is_repeated else [present[field.name]]
            for value in values:
                unset += unset_fields(value, f"{name}.")
    return unset


def sent(statement: Statement) -> pb.Statement:
    return convert.to_response(ParseResult("x.y", "1", statement)).statement


def test_every_field_of_a_statement_reaches_the_proto() -> None:
    response = convert.to_response(ParseResult("x.y", "1", FULL, ["a warning"]))
    assert unset_fields(response) == []
    details = {line.WhichOneof("detail") for account in response.statement.accounts for line in account.lines}
    assert details == {"card", "deposit", "trade", None}


@pytest.mark.parametrize(
    ("values", "mapping"),
    [(AccountKind, convert._ACCOUNT_KINDS), (LineKind, convert._LINE_KINDS)],
    ids=["account-kind", "line-kind"],
)
def test_every_enum_value_has_its_own_proto_value(values: type[Enum], mapping: dict[Enum, int]) -> None:
    assert set(mapping) == set(values)
    assert len(set(mapping.values())) == len(mapping)
    assert 0 not in mapping.values()  # UNSPECIFIED


def test_every_proto_enum_value_has_a_model_value() -> None:
    assert len(convert._ACCOUNT_KINDS) == len(pb.AccountKind.values()) - 1
    assert len(convert._LINE_KINDS) == len(pb.LineKind.values()) - 1


def test_a_line_carries_its_amount_date_kind_and_detail() -> None:
    card, deposit, securities, points = (account.lines[0] for account in sent(FULL).accounts)
    assert (card.amount.value, card.card.original_amount.value, card.card.original_currency) == (
        "28009",
        "780.00",
        "EUR",
    )
    assert (card.date.year, card.date.month, card.date.day, card.kind) == (2026, 10, 8, pb.LINE_KIND_PURCHASE)
    assert (deposit.unit, deposit.deposit.balance.value, deposit.kind) == ("USD", "100.50", pb.LINE_KIND_INTEREST)
    assert (securities.amount.value, securities.kind, securities.trade.net.value) == (
        "-1000",
        pb.LINE_KIND_SELL,
        "996074",
    )
    assert (points.unit, points.description, points.WhichOneof("detail")) == ("小樹點(信用卡)", "本期兌/轉換", None)


def test_a_card_bill_carries_its_due_date_and_total_due() -> None:
    bill = sent(FULL).card_bill
    assert (bill.total_due.value, bill.currency, bill.due_date.month, bill.due_date.day) == ("95730", "TWD", 11, 8)


def test_each_check_goes_out_with_whether_it_balances() -> None:
    checks = [Check("本期消費", "TWD", D(10), D(10)), Check("e point", "e point", D(5), D(6))]
    out = sent(replace(FULL, checks=checks)).checks
    assert [(c.name, c.unit, c.computed.value, c.stated.value, c.balanced) for c in out] == [
        ("本期消費", "TWD", "10", "10", True),
        ("e point", "e point", "5", "6", False),
    ]


def test_unset_optional_fields_are_left_out() -> None:
    payment = Line(date(2026, 9, 23), "TWD", D(-69019), LineKind.PAYMENT, "PAYMENT", CardLine())
    bare = DepositLine(memo="", counterparty="")
    deposit = Line(date(2026, 9, 23), "TWD", D(5), LineKind.OTHER, "ATM", bare)
    buy = replace(TRADE_LINE, detail=replace(TRADE, prepaid=None))
    accounts = [AccountReport(AccountKind.CREDIT_CARD, "", "", lines=[payment, deposit, buy])]
    statement = Statement("hsbc", None, DAY, accounts)
    out = sent(statement)
    assert not out.HasField("period_start")
    assert not out.HasField("card_bill")
    assert list(out.checks) == []
    card, deposit_out, trade = out.accounts[0].lines
    assert card.HasField("card")
    assert card.kind == pb.LINE_KIND_PAYMENT
    assert not card.card.HasField("posting_date")
    assert not card.card.HasField("original_amount")
    assert deposit_out.HasField("deposit")
    assert not deposit_out.deposit.HasField("balance")
    assert not trade.trade.HasField("prepaid")


def test_a_foreign_amount_comes_with_its_currency_and_a_currency_with_its_amount() -> None:
    day = date(2026, 10, 1)

    def line(detail: CardLine) -> Line:
        return Line(day, "TWD", D(300), LineKind.PURCHASE, "SHOP", detail)

    line(CardLine(original_amount=D("9.50"), original_currency="USD"))
    line(CardLine())
    with pytest.raises(LayoutError):
        line(CardLine(original_amount=D("9.50")))
    with pytest.raises(LayoutError):
        line(CardLine(original_currency="USD"))


def test_a_statement_balances_when_every_check_does_and_when_it_has_none() -> None:
    assert FULL.balanced
    assert replace(FULL, checks=[]).balanced
    assert not replace(FULL, checks=[*FULL.checks, Check("繳款小計", "TWD", D(-5), D(-6))]).balanced
    assert FULL.line_count == 4
