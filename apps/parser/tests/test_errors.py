"""Every way a document can fail has its status code (ADR 0005)."""

import grpc
import pytest

from finchie_parser import errors
from finchie_parser.errors import ParserError


def every_error(base: type[ParserError] = ParserError) -> list[type[ParserError]]:
    found = []
    for subclass in base.__subclasses__():
        found += [subclass, *every_error(subclass)]
    return found


@pytest.mark.parametrize("error", every_error(), ids=lambda error: error.__name__)
def test_every_parser_error_has_its_own_status_code(error: type[ParserError]) -> None:
    assert "status" in vars(error), f"{error.__name__} says no status code of its own"
    assert isinstance(error.status, grpc.StatusCode)
    assert error.status is not grpc.StatusCode.UNKNOWN


def test_the_status_codes_are_those_adr_0005_gives_the_backend() -> None:
    assert {error.__name__: error.status for error in every_error()} == {
        "UnsupportedDocumentError": grpc.StatusCode.UNIMPLEMENTED,
        "InvalidDocumentError": grpc.StatusCode.INVALID_ARGUMENT,
        "PasswordRequiredError": grpc.StatusCode.FAILED_PRECONDITION,
        "ConfigurationError": grpc.StatusCode.FAILED_PRECONDITION,
        "LayoutError": grpc.StatusCode.INTERNAL,
        "AbandonedError": grpc.StatusCode.CANCELLED,
    }
    assert errors.ParserError not in every_error()
