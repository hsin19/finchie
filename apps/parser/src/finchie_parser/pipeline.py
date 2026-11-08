from collections.abc import Sequence

from finchie_parser.documents import Mail, read_mail
from finchie_parser.errors import InvalidDocumentError, PasswordRequiredError, UnsupportedDocumentError
from finchie_parser.model import ParseResult
from finchie_parser.ocr import Ocr
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.passwords import Passwords
from finchie_parser.pdf import PdfSet


def parse_document(
    content: bytes,
    *,
    passwords: Passwords,
    ocr: Ocr | None,
    parsers: Sequence[StatementParser] = PARSERS,
) -> ParseResult:
    mail = read_mail(content)
    with PdfSet(mail.pdfs(), passwords) as pdfs:
        index, parser = select_statement(mail, pdfs, parsers)
        parsed = parser.parse(pdfs.open(index, parser), ocr)
    return ParseResult(parser.name, parser.version, parsed.statement, parsed.warnings)


def claimants(mail: Mail, parsers: Sequence[StatementParser]) -> list[StatementParser]:
    """The parsers whose e-mail this is, by sender and subject: one, unless two claim too much."""
    return [parser for parser in parsers if parser.claims(mail)]


def select_statement(mail: Mail, pdfs: PdfSet, parsers: Sequence[StatementParser]) -> tuple[int, StatementParser]:
    """The index of the PDF that is the statement, and its parser. Parsing and previews both pick with it.

    Only a parser that claims the e-mail looks at its PDFs, each opened with that parser's passwords:
    first those whose names it knows, then the others, taking one whose text carries its marker (e-mails
    also carry payment slips and notices, and a name alone proves nothing). Failing that:

    1. A PDF it knows by name but cannot open: `pdfs.open` on it raises why. Its missing password is the
       error to report, not whatever another attachment gave.
    2. Else PasswordRequiredError if another PDF was locked, InvalidDocumentError if one was unreadable,
       UnsupportedDocumentError if none of them is a statement, or if no parser claims the e-mail.
    """
    claimed = claimants(mail, parsers)
    if not claimed:
        raise UnsupportedDocumentError(f"no parser claims an e-mail from {mail.sender_domain!r}: {mail.subject!r}")

    failed: list[PasswordRequiredError | InvalidDocumentError] = []
    for parser in claimed:
        named = [i for i, a in enumerate(pdfs.attachments) if parser.matches_filename(a.filename)]
        unopened: list[tuple[int, PasswordRequiredError | InvalidDocumentError]] = []
        for index in named:
            try:
                if parser.matches_text(pdfs.open(index, parser)):
                    return index, parser
            except (PasswordRequiredError, InvalidDocumentError) as error:
                unopened.append((index, error))
        if unopened:
            return min(unopened, key=lambda pick: _rank(pick[1]))[0], parser
        for index in (i for i in range(len(pdfs)) if i not in named):
            try:
                if parser.matches_text(pdfs.open(index, parser)):
                    return index, parser
            except (PasswordRequiredError, InvalidDocumentError) as error:
                failed.append(error)

    if failed:
        raise min(failed, key=_rank)
    names = ", ".join(attachment.filename for attachment in pdfs.attachments) or "no PDF"
    raise UnsupportedDocumentError(f"{claimed[0].name} claims the e-mail, but none of its PDFs is a statement: {names}")


def _rank(error: PasswordRequiredError | InvalidDocumentError) -> int:
    # A missing password is the one failure the owner can fix.
    return 0 if isinstance(error, PasswordRequiredError) else 1
