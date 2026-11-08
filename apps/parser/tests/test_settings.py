from datetime import date

import pytest

from finchie_parser.settings import Settings

# Synthetic details (ADR 0006).
NATIONAL_ID = "A123456789"


def test_details_and_chosen_passwords_come_from_nested_environment_variables(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("PARSER_IDENTITY__NATIONAL_ID", NATIONAL_ID)
    monkeypatch.setenv("PARSER_IDENTITY__BIRTH_DATE", "2000-01-02")
    monkeypatch.setenv("PARSER_PDF_PASSWORDS__TAISHIN", "abc")
    passwords = Settings(_env_file=None).passwords()
    assert passwords.identity.national_id == NATIONAL_ID
    assert passwords.identity.birth_date == date(2000, 1, 2)
    assert dict(passwords.chosen) == {"taishin": "abc"}


def test_no_details_is_allowed() -> None:
    passwords = Settings(_env_file=None).passwords()
    assert (passwords.identity.national_id, passwords.identity.birth_date, dict(passwords.chosen)) == ("", None, {})


def test_a_malformed_birth_date_is_refused_without_repeating_it() -> None:
    settings = Settings(_env_file=None, identity={"birth_date": "19/12/83"})
    with pytest.raises(ValueError, match="YYYY-MM-DD") as error:
        settings.passwords()
    assert "19/12/83" not in str(error.value)


def test_details_and_passwords_are_not_printed() -> None:
    settings = Settings(
        _env_file=None, identity={"national_id": NATIONAL_ID, "birth_date": "2000-01-02"}, pdf_passwords={"x": "abc"}
    )
    for text in (repr(settings), repr(settings.passwords())):
        assert NATIONAL_ID not in text
        assert "2000" not in text
        assert "abc" not in text
