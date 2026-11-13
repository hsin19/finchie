"""Rewards points on a card bill (ADR 0019): each program a unit of the bank's points account, its lines as the
bill totals them, checked against its closing."""

from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from datetime import date
from decimal import Decimal

from finchie_parser.model import AccountKind, AccountReport, Balance, Check, Expiry, LapseRule, Line, LineKind


@dataclass(frozen=True)
class Move:
    """A period's movement as the bill totals it, under the bill's own label."""

    label: str
    kind: LineKind
    # Seen from the account: added positive, used negative.
    amount: Decimal


@dataclass(frozen=True)
class Program:
    """A program as the bill prints it: its opening, the columns with a figure, and its closing."""

    name: str
    opening: Decimal
    moves: list[Move]
    closing: Decimal
    expiries: list[Expiry]
    # How its points lapse by its terms, where known.
    lapse: LapseRule | None = None


def lapse_months_after_closing(name: str, months: int) -> LapseRule:
    """Each bill's points lapse at the end of the month `months` after its closing month."""
    return LapseRule(name, months_after_closing=months)


def lapse_yearly(name: str, *, last_month: int, years: int) -> LapseRule:
    """What the bills of a year ending with `last_month` add lapse together, at the end of the month after
    it, `years` later."""
    return LapseRule(name, purge_last_month=last_month, purge_years=years)


def earned(label: str, value: Decimal) -> Move:
    return Move(label, LineKind.EARN, value)


def adjusted(label: str, value: Decimal) -> Move:
    return Move(label, LineKind.ADJUST, value)


def used(label: str, value: Decimal) -> Move:
    """Points used, taken by their size: one bank prints them positive, another negative (ADR 0019)."""
    return Move(label, LineKind.REDEEM, -abs(value))


def points_program(
    name: str,
    *,
    opening: Decimal,
    moves: Iterable[Move],
    closing: Decimal,
    expiries: Sequence[Expiry] = (),
    lapse: LapseRule | None = None,
) -> Program:
    """A program as the bill prints it. Only columns with a figure are lines."""
    return Program(name, opening, [move for move in moves if move.amount != 0], closing, list(expiries), lapse)


def points_account(programs: Sequence[Program], closing_date: date) -> tuple[list[AccountReport], list[Check]]:
    """The bank's points account, each program a unit, and each program's check: its opening and its lines
    must give its closing. Neither when the bill has no points."""
    if not programs:
        return [], []
    account = AccountReport(
        kind=AccountKind.POINTS,
        number="",
        name="",
        balances=[Balance(p.name, closing_date, p.closing) for p in programs],
        # The bill totals each column over the period: on the closing date, where its closing is.
        lines=[Line(closing_date, p.name, m.amount, m.kind, m.label) for p in programs for m in p.moves],
        expiries=[expiry for p in programs for expiry in p.expiries],
        lapse_rules=[p.lapse for p in programs if p.lapse is not None],
    )
    checks = [
        Check(p.name, p.name, p.opening + sum((m.amount for m in p.moves), Decimal(0)), p.closing) for p in programs
    ]
    return [account], checks
