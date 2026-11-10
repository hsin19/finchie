"""What statements print, as text: the tokens the bank parsers' patterns are built from, each beside the
function that reads it, and searching a statement's text for a field.

Patterns are strings, to be composed into a parser's own (`rf"^(?P<date>{MONTH_DAY}) …"`). A reader raises
LayoutError for text that is not its token, never a bare ValueError: a parser only reads what its pattern
matched, so anything else means the layout changed (ADR 0005).
"""

import calendar
import re
import unicodedata
from collections.abc import Iterable
from datetime import date, timedelta
from decimal import Decimal, InvalidOperation

from finchie_parser.errors import LayoutError

# Amounts: thousands separated by commas, as every bank prints them. Where a bank's layout says more (two
# decimals, a leading $, never negative), its parser says so in its own pattern.
INTEGER = r"-?\d[\d,]*"
AMOUNT = r"-?\d[\d,]*(?:\.\d+)?"
CENTS = r"-?\d[\d,]*\.\d\d"
# Dates: Gregorian (2026/09/07), Minguo (115/09/07), and month and day alone (09/07, 9/7).
ISO_DATE = r"\d{4}/\d\d/\d\d"
ROC_DATE = r"\d{3}/\d\d/\d\d"
MONTH_DAY = r"\d{1,2}/\d{1,2}"
CURRENCY = r"[A-Z]{3}"
# An account number as statements mask it: 1234567***890.
MASKED_ACCOUNT = r"\d+\*+\d+"

_SPACES = re.compile(r"\s+")
# The Minguo calendar counts years from 1912.
_ROC_OFFSET = 1911


def token(pattern: str) -> re.Pattern[str]:
    """A token alone, for `fullmatch` on a word."""
    return re.compile(pattern, re.ASCII)


def amount(text: str) -> Decimal:
    """`-6,552.97` -> Decimal('-6552.97')."""
    if not re.fullmatch(AMOUNT, text, re.ASCII):
        raise LayoutError(f"not an amount: {text!r}")
    try:
        return Decimal(text.replace(",", ""))
    except InvalidOperation:
        raise LayoutError(f"not an amount: {text!r}") from None


def roc_year(year: int) -> int:
    """A Minguo year as Gregorian: 115 -> 2026."""
    return year + _ROC_OFFSET


def roc_date(text: str) -> date:
    """`115/09/07` (Minguo calendar) -> 2026-09-07."""
    year, month, day = _parts(text, 3)
    return _date(roc_year(year), month, day, text)


def iso_date(text: str) -> date:
    """`2026/10/21` -> 2026-10-21."""
    year, month, day = _parts(text, 3)
    return _date(year, month, day, text)


def either_date(text: str) -> date:
    """`2026/10/21` or `115/10/21`, by the year's digits: a bank that moved from one calendar to the other."""
    return iso_date(text) if len(text.split("/")[0]) == 4 else roc_date(text)


def month_end(year: int, month: int) -> date:
    """The last day of a month: a month's expiring points lapse by it (ADR 0019)."""
    return date(year, month, calendar.monthrange(year, month)[1])


def next_month_end(day: date) -> date:
    """The last day of the month after `day`'s."""
    year, month = (day.year + 1, 1) if day.month == 12 else (day.year, day.month + 1)
    return month_end(year, month)


def month_day(text: str, closing: date) -> date:
    """`09/07` on a card bill: the latest such date at most a month after the statement's closing date.

    Lines fall on both sides of a year end (a December purchase on a January statement, a January posting
    on a December one), and an older line, such as an instalment's original purchase, stays in the past.
    """
    month, day = _parts(text, 2)
    latest = closing + timedelta(days=31)
    for year in (latest.year, latest.year - 1):
        try:
            candidate = date(year, month, day)
        except ValueError:
            continue
        if candidate <= latest:
            return candidate
    raise LayoutError(f"no date {text} in the year up to {latest}")


def day_in_period(text: str, start: date, end: date) -> date:
    """`9/21` on a statement of a period: the day within it, which may span a year end."""
    month, day = _parts(text, 2)
    for year in range(start.year, end.year + 1):
        try:
            candidate = date(year, month, day)
        except ValueError:
            continue
        if start <= candidate <= end:
            return candidate
    raise LayoutError(f"{text} is outside the statement period {start}..{end}")


def clean(text: str) -> str:
    """Full-width letters and digits to ASCII, runs of whitespace to one space."""
    return _SPACES.sub(" ", unicodedata.normalize("NFKC", text)).strip()


def find(pattern: re.Pattern[str], texts: Iterable[str], what: str) -> re.Match[str]:
    """The first line of the pages' text that has `pattern`, or a LayoutError naming the field."""
    for text in texts:
        for line in text.splitlines():
            if match := pattern.search(line):
                return match
    raise LayoutError(f"statement field not found: {what}")


def _parts(text: str, count: int) -> list[int]:
    parts = text.split("/")
    if len(parts) != count or not all(part.isascii() and part.isdigit() for part in parts):
        raise LayoutError(f"not a date: {text!r}")
    return [int(part) for part in parts]


def _date(year: int, month: int, day: int, text: str) -> date:
    try:
        return date(year, month, day)
    except ValueError:
        raise LayoutError(f"no such date: {text!r}") from None
