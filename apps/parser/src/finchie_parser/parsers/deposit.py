"""What every deposit statement parser shares (ADR 0013, ADR 0021): the book of accounts a statement lists,
each checked through its lines against its closing balance, and reading a row's amounts by the columns they
sit under.

A bank's parser only says where its accounts, balances, and lines are printed.
"""

from collections.abc import Callable, Iterable, Iterator, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import date, timedelta
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import AccountKind, AccountReport, Balance, Check, DepositLine, Line, LineKind, Statement
from finchie_parser.pdf import Word
from finchie_parser.text import amount


@dataclass(frozen=True)
class Entry:
    """A line of a deposit account as it is read, before the statement places it in its account and currency."""

    date: date
    description: str
    # Seen from the account: money in is positive.
    amount: Decimal
    kind: LineKind = LineKind.OTHER
    memo: str = ""
    counterparty: str = ""
    # The running balance after the line, when printed.
    balance: Decimal | None = None


@dataclass
class Account:
    """One account in one currency while it is read: its closing balance as listed, then its lines."""

    number: str
    currency: str
    name: str
    closing: Decimal
    # The balance brought forward, where the statement prints one.
    opening: Decimal | None = None
    lines: list[Entry] = field(default_factory=list)


class Accounts:
    """The accounts a statement lists, by number and currency, in the order listed."""

    def __init__(self) -> None:
        self._accounts: dict[tuple[str, str], Account] = {}

    def add(self, number: str, currency: str, name: str, closing: Decimal) -> Account:
        if (number, currency) in self._accounts:
            raise LayoutError(f"{number} {currency} listed twice")
        account = self._accounts[(number, currency)] = Account(number, currency, name, closing)
        return account

    def get(self, number: str, currency: str) -> Account | None:
        return self._accounts.get((number, currency))

    def numbered(self, number: str) -> list[Account]:
        return [account for account in self._accounts.values() if account.number == number]

    def __iter__(self) -> Iterator[Account]:
        return iter(self._accounts.values())

    def __len__(self) -> int:
        return len(self._accounts)

    def statement(self, institution: str, start: date, end: date, closing_label: str) -> Statement:
        """The statement: one report per account number, all its currencies together in the order listed,
        each with its closing balance on `end`, its opening balance (where printed) on the day before `start`,
        and its lines. Each currency is checked from its opening balance, or from its first line's running
        balance, to its closing balance, which the statement prints as `closing_label`."""
        if not self._accounts:
            raise LayoutError("no deposit account listed")
        by_number: dict[str, list[Account]] = {}
        for account in self._accounts.values():
            by_number.setdefault(account.number, []).append(account)
        reports = [_report(currencies, start, end) for currencies in by_number.values()]
        checks = [
            check
            for account in self._accounts.values()
            if (check := reconcile_account(account, closing_label)) is not None
        ]
        return Statement(institution, start, end, reports, checks)


def _report(currencies: Sequence[Account], start: date, end: date) -> AccountReport:
    first = currencies[0]
    if names := sorted({account.name for account in currencies} - {first.name}):
        raise LayoutError(f"{first.number}: named both {first.name!r} and {names}")
    balances = []
    for account in currencies:
        if account.opening is not None:
            balances.append(Balance(account.currency, start - timedelta(days=1), account.opening))
        balances.append(Balance(account.currency, end, account.closing))
    lines = [
        Line(
            entry.date,
            account.currency,
            entry.amount,
            entry.kind,
            entry.description,
            DepositLine(entry.memo, entry.counterparty, entry.balance),
        )
        for account in currencies
        for entry in account.lines
    ]
    return AccountReport(AccountKind.DEPOSIT, first.number, first.name, balances, lines)


def reconcile_account(account: Account, closing_label: str) -> Check | None:
    """Whether an account's lines in one currency account for its closing balance (ADR 0013).

    From the opening balance when printed, else from before the first line by its running balance, each
    line is added in turn. A printed running balance that differs from the sum so far is reported, as the
    first place the lines stop adding up; past the last line, the sum must be the closing balance. None
    when there is nothing to start from. The check is named after the account, so that a statement's
    checks tell its accounts and currencies apart.
    """
    lines = account.lines
    if account.opening is not None:
        running = account.opening
    elif lines and lines[0].balance is not None:
        running = lines[0].balance - lines[0].amount
    else:
        return None
    named = f"{account.name} {account.number} {account.currency}"
    for line in lines:
        running += line.amount
        if line.balance is not None and line.balance != running:
            label = f"{named} {line.date.isoformat()} {line.description} 的餘額"
            return Check(label, account.currency, running, line.balance)
    return Check(f"{named} {closing_label}", account.currency, running, account.closing)


def amounts_by_column(
    words: Iterable[Word],
    column_of: Callable[[Word], str | None],
    where: str,
    value: Callable[[str], Decimal] = amount,
) -> tuple[dict[str, Decimal], list[Word]]:
    """A row's amounts by the column each sits under, and the words that are not one of them.

    `column_of` says which column a word is an amount in, or None for any other word. Two amounts under one
    column mean the columns were read wrong.
    """
    found: dict[str, Decimal] = {}
    rest: list[Word] = []
    for word in words:
        column = column_of(word)
        if column is None:
            rest.append(word)
            continue
        if column in found:
            raise LayoutError(f"{where}: two amounts under {column}")
        found[column] = value(word.text)
    return found, rest


def moved_in(by_column: Mapping[str, Decimal], deposits: str, withdrawals: str) -> Decimal:
    """Money in less money out, from the amounts under a row's deposit and withdrawal columns."""
    return by_column.get(deposits, Decimal(0)) - by_column.get(withdrawals, Decimal(0))


def amount_and_balance(amounts: list[Word], withdrawals: Word, deposits: Word, where: str) -> tuple[Decimal, Decimal]:
    """A row that prints one amount, under either withdrawals or deposits, and its running balance right of
    it: the amount as money in (negative for a withdrawal), by the heading its right edge is nearer, and the
    balance."""
    if len(amounts) != 2:
        raise LayoutError(f"{where}: expected an amount and a balance, found {[w.text for w in amounts]}")
    moved, balance = sorted(amounts, key=lambda w: w.x1)
    out = abs(moved.x1 - withdrawals.x1) < abs(moved.x1 - deposits.x1)
    value = amount(moved.text)
    return -value if out else value, amount(balance.text)
