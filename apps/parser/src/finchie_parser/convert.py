"""Internal model -> generated proto messages."""

from datetime import date
from decimal import Decimal

from google.protobuf import timestamp_pb2
from google.type import date_pb2, decimal_pb2

from finchie.parser.v1 import parser_pb2 as pb
from finchie_parser.model import (
    DepositAccount,
    DepositKind,
    DepositLine,
    DepositStatement,
    Kind,
    ParseResult,
    Reconciliation,
    Statement,
    Transaction,
)
from finchie_parser.reading import OpenedDocument

_KINDS = {
    Kind.PURCHASE: pb.TRANSACTION_KIND_PURCHASE,
    Kind.FOREIGN_TRANSACTION_FEE: pb.TRANSACTION_KIND_FOREIGN_TRANSACTION_FEE,
    Kind.ANNUAL_FEE: pb.TRANSACTION_KIND_ANNUAL_FEE,
    Kind.REFUND: pb.TRANSACTION_KIND_REFUND,
    Kind.REBATE: pb.TRANSACTION_KIND_REBATE,
    Kind.PAYMENT: pb.TRANSACTION_KIND_PAYMENT,
}


_DEPOSIT_KINDS = {
    DepositKind.OTHER: pb.DEPOSIT_LINE_KIND_OTHER,
    DepositKind.INTEREST: pb.DEPOSIT_LINE_KIND_INTEREST,
    DepositKind.FEE: pb.DEPOSIT_LINE_KIND_FEE,
}


def to_response(result: ParseResult) -> pb.ParseDocumentResponse:
    response = pb.ParseDocumentResponse(
        parser=result.parser, parser_version=result.parser_version, warnings=result.warnings
    )
    if isinstance(result.statement, DepositStatement):
        response.deposit_statement.CopyFrom(_deposit_statement(result.statement))
    else:
        response.statement.CopyFrom(_statement(result.statement))
    return response


def to_open_response(opened: OpenedDocument) -> pb.OpenDocumentResponse:
    response = pb.OpenDocumentResponse(
        files=[
            pb.File(
                filename=f.filename, content_type=f.content_type, content=f.data, locked=f.locked, primary=f.primary
            )
            for f in opened.files
        ]
    )
    if (mail := opened.email) is not None:
        response.email.CopyFrom(
            pb.Email(sender=mail.sender, recipient=mail.recipient, subject=mail.subject, html=mail.html, text=mail.text)
        )
        if mail.date is not None:
            stamp = timestamp_pb2.Timestamp()
            stamp.FromDatetime(mail.date)
            response.email.date.CopyFrom(stamp)
    return response


def _statement(statement: Statement) -> pb.Statement:
    reconciliation = statement.reconciliation
    return pb.Statement(
        institution=statement.institution,
        closing_date=_date(statement.closing_date),
        due_date=_date(statement.due_date),
        currency=statement.currency,
        total_due=_decimal(statement.total_due),
        transactions=[_transaction(tx) for tx in statement.transactions],
        reconciliation=_reconciliation(reconciliation),
    )


def _reconciliation(reconciliation: Reconciliation) -> pb.Reconciliation:
    return pb.Reconciliation(
        balanced=reconciliation.balanced,
        computed=_decimal(reconciliation.computed),
        stated=_decimal(reconciliation.stated),
        stated_label=reconciliation.stated_label,
    )


def _deposit_statement(statement: DepositStatement) -> pb.DepositStatement:
    return pb.DepositStatement(
        institution=statement.institution,
        period_start=_date(statement.period_start),
        period_end=_date(statement.period_end),
        accounts=[_deposit_account(account) for account in statement.accounts],
    )


def _deposit_account(account: DepositAccount) -> pb.DepositAccount:
    message = pb.DepositAccount(
        number=account.number,
        name=account.name,
        currency=account.currency,
        closing_balance=_decimal(account.closing_balance),
        lines=[_deposit_line(line) for line in account.lines],
    )
    if account.opening_balance is not None:
        message.opening_balance.CopyFrom(_decimal(account.opening_balance))
    if account.reconciliation is not None:
        message.reconciliation.CopyFrom(_reconciliation(account.reconciliation))
    return message


def _deposit_line(line: DepositLine) -> pb.DepositLine:
    message = pb.DepositLine(
        date=_date(line.date),
        description=line.description,
        memo=line.memo,
        counterparty=line.counterparty,
        amount=_decimal(line.amount),
        kind=_DEPOSIT_KINDS[line.kind],
    )
    if line.balance is not None:
        message.balance.CopyFrom(_decimal(line.balance))
    return message


def _transaction(tx: Transaction) -> pb.Transaction:
    message = pb.Transaction(
        transaction_date=_date(tx.transaction_date),
        description=tx.description,
        kind=_KINDS[tx.kind],
        amount=_decimal(tx.amount),
        original_currency=tx.original_currency,
        country=tx.country,
        card_last4=tx.card_last4,
    )
    if tx.posting_date is not None:
        message.posting_date.CopyFrom(_date(tx.posting_date))
    if tx.original_amount is not None:
        message.original_amount.CopyFrom(_decimal(tx.original_amount))
    return message


def _date(value: date) -> date_pb2.Date:
    return date_pb2.Date(year=value.year, month=value.month, day=value.day)


def _decimal(value: Decimal) -> decimal_pb2.Decimal:
    # Fixed-point notation: google.type.Decimal allows exponents, but consumers rarely parse them.
    return decimal_pb2.Decimal(value=format(value, "f"))
