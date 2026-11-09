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
class PointsExpiry:
    """Points the bill says will lapse by a date (ADR 0019)."""

    amount: Decimal
    by: date


class PointsKind(StrEnum):
    EARN = "earn"
    ADJUST = "adjust"
    REDEEM = "redeem"


@dataclass(frozen=True)
class PointsLine:
    """A period's movement as the bill totals it, under the bill's own label (ADR 0019)."""

    label: str
    kind: PointsKind
    # Seen from the account: added positive, used negative.
    amount: Decimal


@dataclass(frozen=True)
class PointsProgram:
    """A rewards program on a card bill, its points a unit of their own (ADR 0019)."""

    name: str
    closing: Decimal
    expiries: list[PointsExpiry] = field(default_factory=list)
    lines: list[PointsLine] = field(default_factory=list)


@dataclass(frozen=True)
class Statement:
    institution: str
    closing_date: date
    due_date: date
    currency: str
    total_due: Decimal
    transactions: list[Transaction]
    reconciliation: Reconciliation
    points: list[PointsProgram] = field(default_factory=list)


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


class TradeSide(StrEnum):
    BUY = "buy"
    SELL = "sell"


@dataclass(frozen=True)
class SecuritiesTrade:
    date: date
    # Unicode-normalized, as the exchanges spell it (ADR 0017).
    name: str
    side: TradeSide
    # Positive, whichever the side.
    shares: Decimal
    price: Decimal
    category: str
    amount: Decimal
    fee: Decimal
    tax: Decimal
    # Received positive, paid negative; zero for a buy paid in advance.
    net: Decimal
    prepaid: Decimal | None = None


@dataclass(frozen=True)
class SecuritiesHolding:
    name: str
    shares: Decimal


@dataclass(frozen=True)
class SecuritiesStatement:
    institution: str
    account_number: str
    period_start: date
    period_end: date
    holdings_date: date
    # The whole account: a security not listed holds nothing.
    holdings: list[SecuritiesHolding]
    trades: list[SecuritiesTrade]
    reconciliation: Reconciliation

    @property
    def balanced(self) -> bool:
        return self.reconciliation.balanced


AnyStatement = Statement | DepositStatement | SecuritiesStatement


@dataclass(frozen=True)
class ParseResult:
    parser: str
    parser_version: str
    statement: AnyStatement
    warnings: list[str] = field(default_factory=list)
