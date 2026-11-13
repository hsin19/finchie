"""What a statement reports, as `finchie.parser.v2` carries it (ADR 0021): the accounts it reports, each with
its balances on a day and the lines between, the figures it can be checked against, and what only a card bill
has. `convert.py` maps it to the proto field by field; the proto's comments define each field."""

from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal
from enum import StrEnum

from finchie_parser.errors import LayoutError


class AccountKind(StrEnum):
    CREDIT_CARD = "credit_card"
    DEPOSIT = "deposit"
    SECURITIES = "securities"
    POINTS = "points"
    LOAN = "loan"


class LineKind(StrEnum):
    # Card lines.
    PURCHASE = "purchase"
    FOREIGN_TRANSACTION_FEE = "foreign_transaction_fee"
    ANNUAL_FEE = "annual_fee"
    REFUND = "refund"
    REBATE = "rebate"
    PAYMENT = "payment"
    # Deposit lines: only what the bank's own wording says (ADR 0013).
    INTEREST = "interest"
    FEE = "fee"
    OTHER = "other"
    # Trades.
    BUY = "buy"
    SELL = "sell"
    # Points.
    EARN = "earn"
    ADJUST = "adjust"
    REDEEM = "redeem"


@dataclass(frozen=True)
class CardLine:
    posting_date: date | None = None
    original_amount: Decimal | None = None
    original_currency: str = ""
    country: str = ""
    card_last4: str = ""


@dataclass(frozen=True)
class DepositLine:
    memo: str = ""
    counterparty: str = ""
    # The running balance after the line, when printed.
    balance: Decimal | None = None


@dataclass(frozen=True)
class Trade:
    """A trade's money (ADR 0017); its shares are its line's amount."""

    price: Decimal
    category: str
    amount: Decimal
    fee: Decimal
    tax: Decimal
    # Received positive, paid negative; zero for a buy paid in advance.
    net: Decimal
    prepaid: Decimal | None = None


@dataclass(frozen=True)
class Line:
    date: date
    # Money: ISO 4217. A security: its name. Points: the program's name.
    unit: str
    # As the statement prints it for the account (ADR 0021).
    amount: Decimal
    kind: LineKind
    description: str
    detail: CardLine | DepositLine | Trade | None = None

    def __post_init__(self) -> None:
        # A foreign amount means nothing without its currency, nor a currency without the amount: the
        # backend matches refunds in either both or neither (ADR 0009).
        card = self.detail
        if isinstance(card, CardLine) and (card.original_amount is None) != (card.original_currency == ""):
            raise LayoutError(f"{self.date} {self.description!r}: a foreign amount and its currency come together")


@dataclass(frozen=True)
class Balance:
    unit: str
    as_of: date
    amount: Decimal


@dataclass(frozen=True)
class Expiry:
    """Points that will lapse by a date (ADR 0019)."""

    unit: str
    amount: Decimal
    by: date


@dataclass(frozen=True)
class LapseRule:
    """How a program's points lapse by its terms (ADR 0019), from what each bill adds, dated on its closing
    date: at the end of the month `months_after_closing` after the closing month, or once a year, what the
    bills of a year ending with month `purge_last_month` add, at the end of the month after it
    `purge_years` later."""

    unit: str
    months_after_closing: int | None = None
    purge_last_month: int | None = None
    purge_years: int | None = None


@dataclass(frozen=True)
class AccountReport:
    kind: AccountKind
    # Empty for a bank's card and points accounts.
    number: str
    name: str
    balances: list[Balance] = field(default_factory=list)
    lines: list[Line] = field(default_factory=list)
    expiries: list[Expiry] = field(default_factory=list)
    lapse_rules: list[LapseRule] = field(default_factory=list)


@dataclass(frozen=True)
class Check:
    """A figure the statement prints, under its own label, and what the lines it covers come to."""

    name: str
    unit: str
    computed: Decimal
    stated: Decimal

    @property
    def balanced(self) -> bool:
        return self.computed == self.stated


@dataclass(frozen=True)
class CardBill:
    due_date: date
    currency: str
    total_due: Decimal


@dataclass(frozen=True)
class Statement:
    institution: str
    # None when the statement prints no first day, as a card bill.
    period_start: date | None
    closing_date: date
    accounts: list[AccountReport]
    checks: list[Check] = field(default_factory=list)
    card_bill: CardBill | None = None

    @property
    def balanced(self) -> bool:
        """Whether every check balances; one with nothing to check is."""
        return all(check.balanced for check in self.checks)

    @property
    def line_count(self) -> int:
        return sum(len(account.lines) for account in self.accounts)


@dataclass(frozen=True)
class ParseResult:
    parser: str
    parser_version: str
    statement: Statement
    warnings: list[str] = field(default_factory=list)
