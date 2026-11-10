"""The ways a document can fail, each with the gRPC status code that tells the backend whether a retry can
help (ADR 0005). Anything that is not a ParserError is a bug.

Every error a library raises on untrusted input is turned into one of these where the library is called
(`documents.py`, `pdf.py`, `text.py`), so that it reaches the caller with its status, not as a bug.
"""

from typing import ClassVar

import grpc


class ParserError(Exception):
    status: ClassVar[grpc.StatusCode]


class UnsupportedDocumentError(ParserError):
    """No parser recognizes the document. Retrying cannot help."""

    status = grpc.StatusCode.UNIMPLEMENTED


class InvalidDocumentError(ParserError):
    """The bytes are not a readable e-mail or PDF, or one larger than the parser reads."""

    status = grpc.StatusCode.INVALID_ARGUMENT


class PasswordRequiredError(ParserError):
    """The PDF is encrypted and none of the configured passwords open it."""

    status = grpc.StatusCode.FAILED_PRECONDITION


class ConfigurationError(ParserError):
    """The deployment lacks something the statement needs, such as OCR. The owner can fix it; retrying
    without the fix cannot."""

    status = grpc.StatusCode.FAILED_PRECONDITION


class LayoutError(ParserError):
    """A recognized statement no longer matches its parser: the bank changed the layout."""

    status = grpc.StatusCode.INTERNAL


class AbandonedError(ParserError):
    """The caller stopped waiting (its deadline passed or it cancelled), so the work stopped too."""

    status = grpc.StatusCode.CANCELLED
