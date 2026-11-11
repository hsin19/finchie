"""Which of an e-mail's PDFs is the statement, and which parser reads it (ADR 0014). Parsing and previews
both pick with `select_statement` (ADR 0012), from the same PDFs (`Mail.pdfs`)."""

from collections.abc import Sequence
from dataclasses import dataclass
from types import TracebackType
from typing import Self

from finchie_parser.documents import Attachment, Mail
from finchie_parser.errors import InvalidDocumentError, ParserError, PasswordRequiredError, UnsupportedDocumentError
from finchie_parser.parsers import StatementParser
from finchie_parser.passwords import MissingDetailError, Passwords
from finchie_parser.pdf import PdfDocument, decrypt
from finchie_parser.settings import chosen_password_variable, identity_variable

type Unopened = PasswordRequiredError | InvalidDocumentError


class PdfSet:
    """One e-mail's PDFs, each decrypted at most once per parser and opened at most once, closed together.

    A PDF is decrypted with the passwords of the parser that claims the e-mail only (ADR 0014), or with
    none but the empty one when no parser does.
    """

    def __init__(self, attachments: Sequence[Attachment], passwords: Passwords) -> None:
        self.attachments = list(attachments)
        self._passwords = passwords
        self._decrypted: dict[tuple[int, str], bytes | Unopened] = {}
        self._opened: dict[int, PdfDocument] = {}

    def __len__(self) -> int:
        return len(self.attachments)

    def decrypted(self, index: int, parser: StatementParser | None) -> bytes:
        """The PDF without encryption. A PDF that fails raises its error again on every later call."""
        key = (index, parser.name if parser else "")
        if key not in self._decrypted:
            try:
                self._decrypted[key] = self._decrypt(self.attachments[index], parser)
            except (PasswordRequiredError, InvalidDocumentError) as error:
                self._decrypted[key] = error
        result = self._decrypted[key]
        if isinstance(result, bytes):
            return result
        raise result

    def open(self, index: int, parser: StatementParser | None) -> PdfDocument:
        data = self.decrypted(index, parser)
        if index not in self._opened:
            self._opened[index] = PdfDocument(self.attachments[index].filename, data)
        return self._opened[index]

    def _decrypt(self, attachment: Attachment, parser: StatementParser | None) -> bytes:
        if parser is None:
            if (data := decrypt(attachment, [""])) is None:
                raise PasswordRequiredError(f"{attachment.filename}: no parser claims the e-mail, so none opens it")
            return data
        try:
            data = decrypt(attachment, self._passwords.candidates(parser.institution, parser.passwords))
        except MissingDetailError as error:
            # Setting the detail is what would open the PDF.
            raise PasswordRequiredError(
                f"{attachment.filename}: {parser.name} needs {identity_variable(error.detail)}"
            ) from None
        if data is None:
            raise PasswordRequiredError(
                f"{attachment.filename}: none of {parser.name}'s passwords opens it; a password chosen at the bank"
                f" goes in {chosen_password_variable(parser.institution)}"
            )
        return data

    def close(self) -> None:
        for document in self._opened.values():
            document.close()

    def __enter__(self) -> Self:
        return self

    def __exit__(
        self, kind: type[BaseException] | None, error: BaseException | None, traceback: TracebackType | None
    ) -> None:
        self.close()


@dataclass(frozen=True)
class Found:
    """The statement, which its parser's passwords open."""

    index: int
    parser: StatementParser


@dataclass(frozen=True)
class Locked:
    """The statement by its name, which cannot be opened: `error` says why, and is the document's error."""

    index: int
    parser: StatementParser
    error: Unopened


@dataclass(frozen=True)
class NotFound:
    """No PDF is the statement: `error` says why. `parser` is the first that claims the e-mail, if any."""

    error: ParserError
    parser: StatementParser | None


type Selection = Found | Locked | NotFound


def claimants(mail: Mail, parsers: Sequence[StatementParser]) -> list[StatementParser]:
    """The parsers whose e-mail this is, by sender and subject: one, unless two claim too much."""
    return [parser for parser in parsers if parser.claims(mail)]


def select_statement(mail: Mail, pdfs: PdfSet, parsers: Sequence[StatementParser]) -> Selection:
    """The PDF that is the statement, and its parser.

    Only a parser that claims the e-mail looks at its PDFs, each opened with that parser's passwords:
    first those whose names it knows, then the others, taking one whose text carries its marker (e-mails
    also carry payment slips and notices, and a name alone proves nothing). Failing that:

    1. Locked: a PDF it knows by name but cannot open. Its missing password is the error to report, not
       whatever another attachment gave.
    2. NotFound, with PasswordRequiredError if another PDF was locked, InvalidDocumentError if one was
       unreadable, UnsupportedDocumentError if none of them is a statement, or if no parser claims the e-mail.
    """
    claimed = claimants(mail, parsers)
    if not claimed:
        unclaimed = f"no parser claims an e-mail from {mail.sender_domain!r}: {mail.subject!r}"
        return NotFound(UnsupportedDocumentError(unclaimed), None)

    failed: list[Unopened] = []
    for parser in claimed:
        named = [i for i, a in enumerate(pdfs.attachments) if parser.matches_filename(a.filename)]
        unopened: list[Locked] = []
        for index in named:
            try:
                if parser.matches_text(pdfs.open(index, parser)):
                    return Found(index, parser)
            except (PasswordRequiredError, InvalidDocumentError) as error:
                unopened.append(Locked(index, parser, error))
        if unopened:
            return min(unopened, key=lambda locked: _rank(locked.error))
        for index in (i for i in range(len(pdfs)) if i not in named):
            try:
                if parser.matches_text(pdfs.open(index, parser)):
                    return Found(index, parser)
            except (PasswordRequiredError, InvalidDocumentError) as error:
                failed.append(error)

    if failed:
        return NotFound(min(failed, key=_rank), claimed[0])
    names = ", ".join(attachment.filename for attachment in pdfs.attachments) or "no PDF"
    unsupported = f"{claimed[0].name} claims the e-mail, but none of its PDFs is a statement: {names}"
    return NotFound(UnsupportedDocumentError(unsupported), claimed[0])


def _rank(error: Unopened) -> int:
    # A missing password is the one failure the owner can fix.
    return 0 if isinstance(error, PasswordRequiredError) else 1
