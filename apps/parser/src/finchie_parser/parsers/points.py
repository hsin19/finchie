"""Rewards points on a card bill (ADR 0019): a program's lines as the bill totals them, checked."""

from collections.abc import Iterable, Sequence
from decimal import Decimal

from finchie_parser.model import PointsExpiry, PointsKind, PointsLine, PointsProgram


def earned(label: str, value: Decimal) -> PointsLine:
    return PointsLine(label, PointsKind.EARN, value)


def adjusted(label: str, value: Decimal) -> PointsLine:
    return PointsLine(label, PointsKind.ADJUST, value)


def used(label: str, value: Decimal) -> PointsLine:
    """Points used, taken by their size: one bank prints them positive, another negative (ADR 0019)."""
    return PointsLine(label, PointsKind.REDEEM, -abs(value))


def points_program(
    name: str,
    *,
    opening: Decimal,
    lines: Iterable[PointsLine],
    closing: Decimal,
    expiries: Sequence[PointsExpiry] = (),
    warnings: list[str],
) -> PointsProgram:
    """A program as the bill prints it, checked: its opening and its lines must give its closing (ADR 0019).

    Only columns with a figure are lines. When the figures do not add up, a warning is added to `warnings`
    and the closing stays as printed (ADR 0005).
    """
    moved = [line for line in lines if line.amount != 0]
    computed = opening + sum((line.amount for line in moved), Decimal(0))
    if computed != closing:
        warnings.append(f"{name}: opening {opening} plus added less used is {computed}, but the bill prints {closing}")
    return PointsProgram(name, closing, list(expiries), moved)
