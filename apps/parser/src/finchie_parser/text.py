"""Small helpers shared by the bank parsers: ROC dates, amounts, descriptions."""

import calendar
import re
import unicodedata
from datetime import date, timedelta
from decimal import Decimal

from finchie_parser.errors import LayoutError

_SPACES = re.compile(r"\s+")


def roc_date(text: str) -> date:
    """`115/09/07` (Minguo calendar) -> 2026-09-07."""
    year, month, day = (int(part) for part in text.split("/"))
    return date(year + 1911, month, day)


def iso_date(text: str) -> date:
    """`2026/10/21` -> 2026-10-21."""
    year, month, day = (int(part) for part in text.split("/"))
    return date(year, month, day)


def month_end(year: int, month: int) -> date:
    """The last day of a month: a month's expiring points lapse by it (ADR 0019)."""
    return date(year, month, calendar.monthrange(year, month)[1])


def month_day(text: str, closing: date) -> date:
    """`09/07` as the latest such date at most a month after the statement's closing date.

    Lines fall on both sides of a year end (a December purchase on a January
    statement, a January posting on a December one), and an older line, such as
    an instalment's original purchase, stays in the past.
    """
    month, day = (int(part) for part in text.split("/"))
    latest = closing + timedelta(days=31)
    for year in (latest.year, latest.year - 1):
        try:
            candidate = date(year, month, day)
        except ValueError:
            continue
        if candidate <= latest:
            return candidate
    raise LayoutError(f"no date {text} in the year up to {latest}")


def amount(text: str) -> Decimal:
    """`-6,552.97` -> Decimal('-6552.97')."""
    return Decimal(text.replace(",", ""))


def clean(text: str) -> str:
    """Full-width letters and digits to ASCII, runs of whitespace to one space."""
    return _SPACES.sub(" ", unicodedata.normalize("NFKC", text)).strip()
