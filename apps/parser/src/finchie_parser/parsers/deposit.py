"""What every deposit statement parser shares (ADR 0013): the book of accounts a statement lists, each
reconciled through its lines into the statement, and reading a row's amounts by the columns they sit under.

A bank's parser only says where its accounts, balances, and lines are printed.
"""

from collections.abc import Callable, Iterable, Iterator, Mapping
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositAccount, DepositLine, DepositStatement
from finchie_parser.parsers.reconcile import reconcile_account
from finchie_parser.pdf import Word
from finchie_parser.text import amount


@dataclass
class Account:
    """One account in one currency while it is read: its closing balance as listed, then its lines."""

    number: str
    currency: str
    name: str
    closing: Decimal
    # The balance brought forward, where the statement prints one.
    opening: Decimal | None = None
    lines: list[DepositLine] = field(default_factory=list)


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

    def statement(self, institution: str, start: date, end: date, closing_label: str) -> DepositStatement:
        """The statement, each account reconciled from its opening balance, or from its first line's running
        balance, to its closing balance, which the statement prints as `closing_label`."""
        if not self._accounts:
            raise LayoutError("no deposit account listed")
        accounts = [
            DepositAccount(
                number=account.number,
                name=account.name,
                currency=account.currency,
                closing_balance=account.closing,
                lines=account.lines,
                reconciliation=reconcile_account(account.opening, account.closing, account.lines, closing_label),
                opening_balance=account.opening,
            )
            for account in self._accounts.values()
        ]
        return DepositStatement(institution, start, end, accounts)


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
