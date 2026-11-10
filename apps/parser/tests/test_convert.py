"""The model as the proto carries it: every enum value has its proto value, and every field reaches the wire.
Amounts are synthetic (ADR 0006)."""

from dataclasses import replace
from datetime import date
from decimal import Decimal
from enum import Enum

import pytest
from google.protobuf.message import Message

from finchie.parser.v1 import parser_pb2 as pb
from finchie_parser import convert
from finchie_parser.model import (
    AnyStatement,
    DepositAccount,
    DepositKind,
    DepositLine,
    DepositStatement,
    Kind,
    ParseResult,
    PointsExpiry,
    PointsKind,
    PointsLine,
    PointsProgram,
    Reconciliation,
    SecuritiesHolding,
    SecuritiesStatement,
    SecuritiesTrade,
    Statement,
    TradeSide,
    Transaction,
)

D = Decimal
RECONCILIATION = Reconciliation(D(10), D(10), "合計")

# Every field set, to something other than its default.
CARD_BILL = Statement(
    institution="hsbc",
    closing_date=date(2026, 10, 21),
    due_date=date(2026, 11, 8),
    currency="TWD",
    total_due=D("95730"),
    transactions=[
        Transaction(
            transaction_date=date(2026, 10, 8),
            posting_date=date(2026, 10, 9),
            description="SHOP",
            kind=Kind.PURCHASE,
            amount=D(28009),
            original_amount=D("780.00"),
            original_currency="EUR",
            country="IT",
            card_last4="1234",
        ),
    ],
    reconciliation=RECONCILIATION,
    points=[
        PointsProgram(
            "小樹點(信用卡)",
            D(3206),
            [PointsExpiry(D(88), date(2026, 4, 30))],
            [PointsLine("本期兌/轉換", PointsKind.REDEEM, D(-1))],
        )
    ],
)
DEPOSIT = DepositStatement(
    "taishin",
    date(2026, 9, 1),
    date(2026, 9, 30),
    [
        DepositAccount(
            number="123456****7890",
            name="新臺幣活存",
            currency="TWD",
            closing_balance=D("100.50"),
            lines=[
                DepositLine(
                    date(2026, 9, 21),
                    "存款息",
                    D("12"),
                    kind=DepositKind.INTEREST,
                    memo="note",
                    counterparty="808 0012345",
                    balance=D("100.50"),
                )
            ],
            reconciliation=RECONCILIATION,
            opening_balance=D("88.50"),
        )
    ],
)
SECURITIES = SecuritiesStatement(
    institution="skis",
    account_number="1234***5",
    period_start=date(2026, 7, 1),
    period_end=date(2026, 7, 31),
    holdings_date=date(2026, 8, 4),
    holdings=[SecuritiesHolding("台積電", D(1000))],
    trades=[
        SecuritiesTrade(
            date=date(2026, 7, 3),
            name="台積電",
            side=TradeSide.BUY,
            shares=D(1000),
            price=D("1000.5"),
            category="現買",
            amount=D(1000500),
            fee=D(1425),
            tax=D(1),
            net=D(0),
            prepaid=D(1001925),
        )
    ],
    reconciliation=RECONCILIATION,
)


def unset_fields(message: Message, path: str = "") -> list[str]:
    """Every field of `message` left at its default, through nested and repeated messages."""
    unset = []
    present = {field.name: value for field, value in message.ListFields()}
    for field in message.DESCRIPTOR.fields:
        name = f"{path}{field.name}"
        if field.name not in present:
            unset.append(name)
        elif field.message_type is not None:
            values = present[field.name] if field.is_repeated else [present[field.name]]
            for value in values:
                unset += unset_fields(value, f"{name}.")
    return unset


@pytest.mark.parametrize(
    ("statement", "sent_as"),
    [(CARD_BILL, "statement"), (DEPOSIT, "deposit_statement"), (SECURITIES, "securities_statement")],
    ids=["card", "deposit", "securities"],
)
def test_every_field_of_a_statement_reaches_the_proto(statement: AnyStatement, sent_as: str) -> None:
    response = convert.to_response(ParseResult("x.y", "1", statement, ["a warning"]))
    sent = getattr(response, sent_as)
    assert unset_fields(sent) == []
    others = {"statement", "deposit_statement", "securities_statement"} - {sent_as}
    assert not any(response.HasField(other) for other in others)


@pytest.mark.parametrize(
    ("values", "mapping"),
    [
        (Kind, convert._KINDS),
        (DepositKind, convert._DEPOSIT_KINDS),
        (PointsKind, convert._POINTS_KINDS),
        (TradeSide, convert._TRADE_SIDES),
    ],
    ids=["transaction", "deposit-line", "points-line", "trade-side"],
)
def test_every_enum_value_has_its_own_proto_value(values: type[Enum], mapping: dict[Enum, int]) -> None:
    assert set(mapping) == set(values)
    assert len(set(mapping.values())) == len(mapping)
    assert 0 not in mapping.values()  # UNSPECIFIED


def test_a_card_bill_carries_its_amounts_dates_and_points() -> None:
    sent = convert.to_response(ParseResult("hsbc.credit_card", "1", CARD_BILL)).statement
    assert sent.total_due.value == "95730"
    assert sent.reconciliation.balanced
    (shop,) = sent.transactions
    assert (shop.amount.value, shop.original_amount.value, shop.original_currency) == ("28009", "780.00", "EUR")
    assert (shop.transaction_date.year, shop.transaction_date.month, shop.transaction_date.day) == (2026, 10, 8)
    assert shop.kind == pb.TRANSACTION_KIND_PURCHASE
    [program] = sent.points
    [expiry] = program.expiries
    assert (expiry.amount.value, expiry.by.year, expiry.by.month, expiry.by.day) == ("88", 2026, 4, 30)
    [line] = program.lines
    assert (line.label, line.kind, line.amount.value) == ("本期兌/轉換", pb.POINTS_LINE_KIND_REDEEM, "-1")


def test_unset_optional_fields_are_left_out() -> None:
    payment = Transaction(date(2026, 9, 23), "PAYMENT", Kind.PAYMENT, D(-69019))
    bill = Statement("hsbc", date(2026, 10, 21), date(2026, 11, 8), "TWD", D(0), [payment], RECONCILIATION)
    (sent,) = convert.to_response(ParseResult("hsbc.credit_card", "1", bill)).statement.transactions
    assert sent.kind == pb.TRANSACTION_KIND_PAYMENT
    assert not sent.HasField("posting_date")
    assert not sent.HasField("original_amount")
    statement = replace(SECURITIES, trades=[replace(SECURITIES.trades[0], prepaid=None)])
    (sent_trade,) = convert.to_response(ParseResult("skis.securities", "1", statement)).securities_statement.trades
    assert not sent_trade.HasField("prepaid")


def test_a_trade_goes_out_with_its_side_and_prepayment() -> None:
    (sent,) = convert.to_response(ParseResult("skis.securities", "1", SECURITIES)).securities_statement.trades
    assert (sent.side, sent.prepaid.value, sent.price.value) == (pb.TRADE_SIDE_BUY, "1001925", "1000.5")


def test_a_foreign_amount_comes_with_its_currency_and_a_currency_with_its_amount() -> None:
    from finchie_parser.errors import LayoutError
    from finchie_parser.model import Kind, Transaction

    day = date(2026, 10, 1)
    Transaction(day, "SHOP", Kind.PURCHASE, Decimal("300"), original_amount=Decimal("9.50"), original_currency="USD")
    Transaction(day, "SHOP", Kind.PURCHASE, Decimal("300"))
    with pytest.raises(LayoutError):
        Transaction(day, "SHOP", Kind.PURCHASE, Decimal("300"), original_amount=Decimal("9.50"))
    with pytest.raises(LayoutError):
        Transaction(day, "SHOP", Kind.PURCHASE, Decimal("300"), original_currency="USD")
