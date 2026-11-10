"""Internal model -> generated proto messages, field by field. Every enum value has its proto value, and every
field of the model reaches the wire (test_convert.py checks both)."""

from datetime import date
from decimal import Decimal
from typing import assert_never

from google.protobuf import timestamp_pb2
from google.type import date_pb2, decimal_pb2

from finchie.parser.v2 import parser_pb2 as pb
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
from finchie_parser.reading import OpenedDocument

_ACCOUNT_KINDS = {
    AccountKind.CREDIT_CARD: pb.ACCOUNT_KIND_CREDIT_CARD,
    AccountKind.DEPOSIT: pb.ACCOUNT_KIND_DEPOSIT,
    AccountKind.SECURITIES: pb.ACCOUNT_KIND_SECURITIES,
    AccountKind.POINTS: pb.ACCOUNT_KIND_POINTS,
    AccountKind.LOAN: pb.ACCOUNT_KIND_LOAN,
}

_LINE_KINDS = {
    LineKind.PURCHASE: pb.LINE_KIND_PURCHASE,
    LineKind.FOREIGN_TRANSACTION_FEE: pb.LINE_KIND_FOREIGN_TRANSACTION_FEE,
    LineKind.ANNUAL_FEE: pb.LINE_KIND_ANNUAL_FEE,
    LineKind.REFUND: pb.LINE_KIND_REFUND,
    LineKind.REBATE: pb.LINE_KIND_REBATE,
    LineKind.PAYMENT: pb.LINE_KIND_PAYMENT,
    LineKind.INTEREST: pb.LINE_KIND_INTEREST,
    LineKind.FEE: pb.LINE_KIND_FEE,
    LineKind.OTHER: pb.LINE_KIND_OTHER,
    LineKind.BUY: pb.LINE_KIND_BUY,
    LineKind.SELL: pb.LINE_KIND_SELL,
    LineKind.EARN: pb.LINE_KIND_EARN,
    LineKind.ADJUST: pb.LINE_KIND_ADJUST,
    LineKind.REDEEM: pb.LINE_KIND_REDEEM,
}


def to_response(result: ParseResult) -> pb.ParseDocumentResponse:
    return pb.ParseDocumentResponse(
        parser=result.parser,
        parser_version=result.parser_version,
        statement=_statement(result.statement),
        warnings=result.warnings,
    )


def to_open_response(opened: OpenedDocument) -> pb.OpenDocumentResponse:
    response = pb.OpenDocumentResponse(
        files=[
            pb.File(
                filename=f.filename, content_type=f.content_type, content=f.data, locked=f.locked, primary=f.primary
            )
            for f in opened.files
        ]
    )
    mail = opened.email
    response.email.CopyFrom(
        pb.Email(sender=mail.sender, recipient=mail.recipient, subject=mail.subject, html=mail.html, text=mail.text)
    )
    if mail.date is not None:
        stamp = timestamp_pb2.Timestamp()
        stamp.FromDatetime(mail.date)
        response.email.date.CopyFrom(stamp)
    return response


def _statement(statement: Statement) -> pb.Statement:
    message = pb.Statement(
        institution=statement.institution,
        closing_date=_date(statement.closing_date),
        accounts=[_account(account) for account in statement.accounts],
        checks=[_check(check) for check in statement.checks],
    )
    if statement.period_start is not None:
        message.period_start.CopyFrom(_date(statement.period_start))
    if statement.card_bill is not None:
        message.card_bill.CopyFrom(_card_bill(statement.card_bill))
    return message


def _card_bill(bill: CardBill) -> pb.CardBill:
    return pb.CardBill(due_date=_date(bill.due_date), currency=bill.currency, total_due=_decimal(bill.total_due))


def _account(account: AccountReport) -> pb.AccountReport:
    return pb.AccountReport(
        kind=_ACCOUNT_KINDS[account.kind],
        number=account.number,
        name=account.name,
        balances=[_balance(balance) for balance in account.balances],
        lines=[_line(line) for line in account.lines],
        expiries=[_expiry(expiry) for expiry in account.expiries],
    )


def _balance(balance: Balance) -> pb.Balance:
    return pb.Balance(unit=balance.unit, as_of=_date(balance.as_of), amount=_decimal(balance.amount))


def _expiry(expiry: Expiry) -> pb.Expiry:
    return pb.Expiry(unit=expiry.unit, amount=_decimal(expiry.amount), by=_date(expiry.by))


def _check(check: Check) -> pb.Check:
    return pb.Check(
        name=check.name,
        unit=check.unit,
        computed=_decimal(check.computed),
        stated=_decimal(check.stated),
        balanced=check.balanced,
    )


def _line(line: Line) -> pb.Line:
    message = pb.Line(
        date=_date(line.date),
        unit=line.unit,
        amount=_decimal(line.amount),
        kind=_LINE_KINDS[line.kind],
        description=line.description,
    )
    match line.detail:
        case CardLine() as card:
            message.card.CopyFrom(_card_line(card))
        case DepositLine() as deposit:
            message.deposit.CopyFrom(_deposit_line(deposit))
        case Trade() as trade:
            message.trade.CopyFrom(_trade(trade))
        case None:
            pass
        case unknown:
            assert_never(unknown)
    return message


def _card_line(card: CardLine) -> pb.CardLine:
    message = pb.CardLine(original_currency=card.original_currency, country=card.country, card_last4=card.card_last4)
    if card.posting_date is not None:
        message.posting_date.CopyFrom(_date(card.posting_date))
    if card.original_amount is not None:
        message.original_amount.CopyFrom(_decimal(card.original_amount))
    return message


def _deposit_line(deposit: DepositLine) -> pb.DepositLine:
    message = pb.DepositLine(memo=deposit.memo, counterparty=deposit.counterparty)
    if deposit.balance is not None:
        message.balance.CopyFrom(_decimal(deposit.balance))
    return message


def _trade(trade: Trade) -> pb.Trade:
    message = pb.Trade(
        price=_decimal(trade.price),
        category=trade.category,
        amount=_decimal(trade.amount),
        fee=_decimal(trade.fee),
        tax=_decimal(trade.tax),
        net=_decimal(trade.net),
    )
    if trade.prepaid is not None:
        message.prepaid.CopyFrom(_decimal(trade.prepaid))
    return message


def _date(value: date) -> date_pb2.Date:
    return date_pb2.Date(year=value.year, month=value.month, day=value.day)


def _decimal(value: Decimal) -> decimal_pb2.Decimal:
    # Fixed-point notation: google.type.Decimal allows exponents, but consumers rarely parse them.
    return decimal_pb2.Decimal(value=format(value, "f"))
