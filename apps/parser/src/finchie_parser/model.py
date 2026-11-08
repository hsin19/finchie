from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal
from enum import StrEnum


class Kind(StrEnum):
    PURCHASE = "purchase"
    FOREIGN_TRANSACTION_FEE = "foreign_transaction_fee"
    ANNUAL_FEE = "annual_fee"
    REFUND = "refund"
    REBATE = "rebate"
    PAYMENT = "payment"


@dataclass(frozen=True)
class Transaction:
    transaction_date: date
    description: str
    kind: Kind
    amount: Decimal
    posting_date: date | None = None
    original_amount: Decimal | None = None
    original_currency: str = ""
    country: str = ""
    card_last4: str = ""


@dataclass(frozen=True)
class Reconciliation:
    computed: Decimal
    stated: Decimal
    stated_label: str

    @property
    def balanced(self) -> bool:
        return self.computed == self.stated


@dataclass(frozen=True)
class Statement:
    institution: str
    closing_date: date
    due_date: date
    currency: str
    total_due: Decimal
    transactions: list[Transaction]
    reconciliation: Reconciliation


class DepositKind(StrEnum):
    """Only what the bank's own wording says (ADR 0013)."""

    OTHER = "other"
    INTEREST = "interest"
    FEE = "fee"


@dataclass(frozen=True)
class DepositLine:
    date: date
    description: str
    # Seen from the account: money in is positive.
    amount: Decimal
    kind: DepositKind = DepositKind.OTHER
    memo: str = ""
    counterparty: str = ""
    # The running balance after the line, when printed.
    balance: Decimal | None = None


@dataclass(frozen=True)
class DepositAccount:
    """One account in one currency, as a deposit statement shows it."""

    number: str
    name: str
    currency: str
    closing_balance: Decimal
    lines: list[DepositLine]
    # None when there is nothing to add up: no opening balance and no running balance printed.
    reconciliation: Reconciliation | None
    opening_balance: Decimal | None = None


@dataclass(frozen=True)
class DepositStatement:
    institution: str
    period_start: date
    period_end: date
    accounts: list[DepositAccount]

    @property
    def balanced(self) -> bool:
        return all(a.reconciliation is None or a.reconciliation.balanced for a in self.accounts)


@dataclass(frozen=True)
class ParseResult:
    parser: str
    parser_version: str
    statement: Statement | DepositStatement
    warnings: list[str] = field(default_factory=list)
