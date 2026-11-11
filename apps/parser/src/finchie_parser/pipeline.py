"""Parse a document: read the e-mail, select the statement among its PDFs, and have its parser read it."""

from collections.abc import Callable, Sequence

from opentelemetry import trace

from finchie_parser.documents import read_mail
from finchie_parser.errors import AbandonedError, ConfigurationError
from finchie_parser.model import ParseResult
from finchie_parser.ocr import Ocr, OcrLine
from finchie_parser.parsers import PARSERS, StatementParser
from finchie_parser.passwords import Passwords
from finchie_parser.pdf import Document
from finchie_parser.selection import Found, Locked, NotFound, PdfSet, select_statement
from finchie_parser.settings import variable

_tracer = trace.get_tracer(__name__)


def parse_document(
    content: bytes,
    *,
    passwords: Passwords,
    ocr: Ocr | None,
    parsers: Sequence[StatementParser] = PARSERS,
    wanted: Callable[[], bool] = lambda: True,
) -> ParseResult:
    """The statement in the e-mail `content`. `wanted` says whether the caller still waits for it: once it
    does not, the work stops between steps and before each OCR call with AbandonedError."""
    mail = read_mail(content)
    with PdfSet(mail.pdfs(), passwords) as pdfs:
        # Spans for the two steps that take time: decrypting and reading PDFs, and parsing with any OCR.
        with _tracer.start_as_current_span("select_statement"):
            selection = select_statement(mail, pdfs, parsers)
        match selection:
            case Found(index, parser):
                document = pdfs.open(index, parser)
            case Locked(error=error) | NotFound(error=error):
                raise error
        if parser.needs_ocr and ocr is None:
            raise ConfigurationError(f"{parser.name} needs OCR, which {variable('ocr')}=false turns off")
        _check(wanted)
        with _tracer.start_as_current_span("parse", attributes={"finchie.parser.name": parser.name}):
            parsed = parser.parse(document, _WantedOcr(ocr, wanted) if ocr is not None else None)
    return ParseResult(parser.name, parser.version, parsed.statement, parsed.warnings)


class _WantedOcr:
    """OCR, the slowest step, only while the caller still waits."""

    def __init__(self, ocr: Ocr, wanted: Callable[[], bool]) -> None:
        self._ocr = ocr
        self._wanted = wanted

    def read_column(self, document: Document, page_index: int, left: float, right: float) -> list[OcrLine]:
        _check(self._wanted)
        return self._ocr.read_column(document, page_index, left, right)


def _check(wanted: Callable[[], bool]) -> None:
    if not wanted():
        raise AbandonedError("the caller stopped waiting")
