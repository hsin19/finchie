import re
from abc import ABC, abstractmethod
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, field
from decimal import Decimal
from typing import ClassVar

from finchie_parser.documents import Mail
from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositLine, DepositStatement, Kind, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document


@dataclass
class Parsed:
    statement: Statement | DepositStatement
    warnings: list[str] = field(default_factory=list)


class StatementParser(ABC):
    """One kind of statement e-mail: it claims the e-mail, opens its PDF, and reads it (ADR 0014).

    Parsers are stateless and shared across threads.
    """

    name: ClassVar[str]
    # Bump when the output for the same input can change, so the backend knows what to re-parse.
    version: ClassVar[str]
    institution: ClassVar[str]
    # The e-mails it claims: from one of these domains or a subdomain of one, with a subject that matches.
    sender_domains: ClassVar[tuple[str, ...]]
    subject_pattern: ClassVar[re.Pattern[str]]
    # Its statement among the e-mail's attachments, tried before the others.
    filename_pattern: ClassVar[re.Pattern[str]]
    # Text that only this bank's statements contain on the first page.
    text_marker: ClassVar[str]

    def claims(self, mail: Mail) -> bool:
        domain = mail.sender_domain
        from_bank = any(domain == d or domain.endswith(f".{d}") for d in self.sender_domains)
        return from_bank and bool(self.subject_pattern.search(mail.subject))

    def matches_filename(self, filename: str) -> bool:
        return bool(self.filename_pattern.search(filename))

    def matches_text(self, document: Document) -> bool:
        return any(self.text_marker in text for text in document.page_texts(1))

    @abstractmethod
    def passwords(self, identity: Identity) -> list[str]:
        """The bank's rule for its PDF password, as its e-mail states it. Tried after no password and after
        one the owner chose (`Passwords.candidates`)."""

    @abstractmethod
    def parse(self, document: Document, ocr: Ocr | None) -> Parsed: ...


def sum_new_charges(transactions: Iterable[Transaction]) -> Decimal:
    return sum((tx.amount for tx in transactions if tx.kind is not Kind.PAYMENT), Decimal(0))


def find(pattern: re.Pattern[str], texts: Iterable[str], what: str) -> re.Match[str]:
    for text in texts:
        for line in text.splitlines():
            if match := pattern.search(line):
                return match
    raise LayoutError(f"statement field not found: {what}")


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
