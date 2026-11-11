"""What every card bill parser shares (ADR 0021): a bill reports the bank's card account, holding its lines,
and its points account, and carries its own facts as a CardBill. A bank's parser only says where its lines
and figures are printed, and which figures to check."""

from collections.abc import Iterable, Sequence
from datetime import date
from decimal import Decimal

from finchie_parser.model import AccountKind, AccountReport, CardBill, CardLine, Check, Line, LineKind, Statement
from finchie_parser.parsers.points import Program, points_account

# Every bill read so far is in NT$.
CURRENCY = "TWD"


def card_line(
    day: date,
    description: str,
    kind: LineKind,
    amount: Decimal,
    *,
    posting_date: date | None = None,
    original_amount: Decimal | None = None,
    original_currency: str = "",
    country: str = "",
    card_last4: str = "",
) -> Line:
    """A line of the bill, in its currency, as printed: charges positive, credits negative."""
    detail = CardLine(posting_date, original_amount, original_currency, country, card_last4)
    return Line(day, CURRENCY, amount, kind, description, detail)


def new_charges(lines: Iterable[Line]) -> Decimal:
    """A card bill's new charges: every line but its payments."""
    return sum((line.amount for line in lines if line.kind is not LineKind.PAYMENT), Decimal(0))


def payments(lines: Iterable[Line]) -> Decimal:
    return sum((line.amount for line in lines if line.kind is LineKind.PAYMENT), Decimal(0))


def check(name: str, computed: Decimal, stated: Decimal) -> Check:
    """A figure of the bill, in its currency, under the bill's own label."""
    return Check(name, CURRENCY, computed, stated)


def card_bill(
    institution: str,
    *,
    closing: date,
    due: date,
    total_due: Decimal,
    lines: list[Line],
    checks: Sequence[Check],
    programs: Sequence[Program] = (),
) -> Statement:
    """The bill: the card account with its lines and no balance (the caller takes it from the total due,
    ADR 0022), the points account when the bill has points, and every check, the points programs' last."""
    points, points_checks = points_account(programs, closing)
    return Statement(
        institution=institution,
        period_start=None,
        closing_date=closing,
        accounts=[AccountReport(AccountKind.CREDIT_CARD, number="", name="", lines=lines), *points],
        checks=[*checks, *points_checks],
        card_bill=CardBill(due, CURRENCY, total_due),
    )
