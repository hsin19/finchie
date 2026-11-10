"""The bank parser interface, and the registry's rules for it."""

import re
from abc import ABC, abstractmethod
from collections.abc import Sequence
from dataclasses import dataclass, field
from typing import ClassVar

from finchie_parser.documents import Mail
from finchie_parser.model import AnyStatement
from finchie_parser.ocr import Ocr
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document


@dataclass
class Parsed:
    statement: AnyStatement
    warnings: list[str] = field(default_factory=list)


class StatementParser(ABC):
    """One kind of statement e-mail: it claims the e-mail, opens its PDF, and reads it (ADR 0014).

    Parsers are stateless and shared across threads. `registry` checks the metadata below when the parsers
    are registered.
    """

    name: ClassVar[str]
    # Bump when the output for the same input can change, so the backend knows what to re-parse. That
    # includes a change to the shared code under parsers/ that changes this parser's output.
    version: ClassVar[str]
    institution: ClassVar[str]
    # The e-mails it claims: from one of these domains or a subdomain of one, with every one of these
    # terms in the subject. Terms, not a pattern: the Gmail filter searches for the same ones (ADR 0014).
    sender_domains: ClassVar[tuple[str, ...]]
    subject_terms: ClassVar[tuple[str, ...]]
    # Its statement among the e-mail's attachments, tried before the others.
    filename_pattern: ClassVar[re.Pattern[str]]
    # Text on the first page that tells its statement from the other PDFs of the same e-mail, such as a
    # payment slip. Only the parser that claims the e-mail looks, so it need not be unique across banks.
    text_marker: ClassVar[str]
    # Whether it cannot read its statements without OCR (PARSER_OCR).
    needs_ocr: ClassVar[bool] = False

    def claims(self, mail: Mail) -> bool:
        domain = mail.sender_domain
        from_bank = any(domain == d or domain.endswith(f".{d}") for d in self.sender_domains)
        # Case aside, as Gmail searches.
        subject = mail.subject.casefold()
        return from_bank and all(term.casefold() in subject for term in self.subject_terms)

    def matches_filename(self, filename: str) -> bool:
        return bool(self.filename_pattern.search(filename))

    def matches_text(self, document: Document) -> bool:
        return any(self.text_marker in text for text in document.page_texts(1))

    @abstractmethod
    def passwords(self, identity: Identity) -> list[str]:
        """The bank's rule for its PDF password, as its e-mail states it. Tried after no password and after
        one the owner chose (`Passwords.candidates`)."""

    @abstractmethod
    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        """The statement. `ocr` is None when OCR is turned off; never for a parser that `needs_ocr`."""


_METADATA: dict[str, type | tuple[type, ...]] = {
    "name": str,
    "version": str,
    "institution": str,
    "sender_domains": tuple,
    "subject_terms": tuple,
    "filename_pattern": re.Pattern,
    "text_marker": str,
}


def registry(*parsers: StatementParser) -> tuple[StatementParser, ...]:
    """The parsers, in the order they are tried, once their metadata is checked: every field set, versions
    numbers, names unique, institutions lower case (they name PARSER_PDF_PASSWORDS__<INSTITUTION>)."""
    for parser in parsers:
        kind = type(parser).__name__
        for attribute, expected in _METADATA.items():
            value = getattr(parser, attribute, None)
            if not isinstance(value, expected) or not (value.pattern if isinstance(value, re.Pattern) else value):
                raise TypeError(f"{kind}.{attribute} must be a non-empty {expected}")
        if not parser.version.isdigit():
            raise ValueError(f"{kind}.version must be a number, not {parser.version!r}")
        if parser.institution != parser.institution.lower() or not parser.institution.isidentifier():
            raise ValueError(f"{kind}.institution must be a lower-case identifier, not {parser.institution!r}")
        if not all(isinstance(item, str) and item for item in (*parser.sender_domains, *parser.subject_terms)):
            raise TypeError(f"{kind}: sender domains and subject terms must be non-empty strings")
    names = [parser.name for parser in parsers]
    if duplicates := sorted({name for name in names if names.count(name) > 1}):
        raise ValueError(f"parser names registered twice: {duplicates}")
    return tuple(parsers)


def institutions(parsers: Sequence[StatementParser]) -> frozenset[str]:
    return frozenset(parser.institution for parser in parsers)
