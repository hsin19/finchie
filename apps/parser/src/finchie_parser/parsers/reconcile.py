"""Reconciliation: whether a statement's lines add up to what it prints (ADR 0005, 0013). Each parser picks
the figures to check; these are the checks more than one of them makes."""

from collections.abc import Iterable, Sequence
from decimal import Decimal

from finchie_parser.model import DepositLine, Kind, Reconciliation, Transaction


def sum_new_charges(transactions: Iterable[Transaction]) -> Decimal:
    """A card bill's new charges: every line but its payments."""
    return sum((tx.amount for tx in transactions if tx.kind is not Kind.PAYMENT), Decimal(0))


def reconcile_account(
    opening: Decimal | None, closing: Decimal, lines: Sequence[DepositLine], closing_label: str
) -> Reconciliation | None:
    """Whether a deposit account's lines account for its closing balance (ADR 0013).

    From the opening balance when printed, else from before the first line by its running balance, each
    line is added in turn. A printed running balance that differs from the sum so far is reported, as the
    first place the lines stop adding up; past the last line, the sum must be the closing balance. None
    when there is nothing to start from.
    """
    if opening is not None:
        running = opening
    elif lines and lines[0].balance is not None:
        running = lines[0].balance - lines[0].amount
    else:
        return None
    for line in lines:
        running += line.amount
        if line.balance is not None and line.balance != running:
            return Reconciliation(running, line.balance, f"{line.date.isoformat()} {line.description} 的餘額")
    return Reconciliation(running, closing, closing_label)
