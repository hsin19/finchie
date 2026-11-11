from datetime import date
from pathlib import Path

import pytest

from finchie_parser.settings import Settings, chosen_password_variable, identity_variable, variable

# Synthetic details (ADR 0006).
NATIONAL_ID = "A123456789"
INSTITUTIONS = ("taishin", "esun")


def test_details_and_chosen_passwords_come_from_nested_environment_variables(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("PARSER_IDENTITY__NATIONAL_ID", NATIONAL_ID)
    monkeypatch.setenv("PARSER_IDENTITY__BIRTH_DATE", "2000-01-02")
    monkeypatch.setenv("PARSER_PDF_PASSWORDS__TAISHIN", "abc")
    passwords = Settings().passwords(INSTITUTIONS)
    assert passwords.identity.national_id == NATIONAL_ID
    assert passwords.identity.birth_date == date(2000, 1, 2)
    assert dict(passwords.chosen) == {"taishin": "abc"}


def test_no_details_is_allowed() -> None:
    passwords = Settings().passwords(INSTITUTIONS)
    assert (passwords.identity.national_id, passwords.identity.birth_date, dict(passwords.chosen)) == ("", None, {})


def test_a_chosen_password_for_an_institution_no_parser_has_is_refused_by_its_variable() -> None:
    settings = Settings(pdf_passwords={"taishn": "abc"})
    with pytest.raises(ValueError, match="PARSER_PDF_PASSWORDS__TAISHN") as error:
        settings.passwords(INSTITUTIONS)
    assert "abc" not in str(error.value)


def test_a_malformed_birth_date_is_refused_without_repeating_it() -> None:
    settings = Settings(identity={"birth_date": "19/12/83"})
    with pytest.raises(ValueError, match="PARSER_IDENTITY__BIRTH_DATE must be YYYY-MM-DD") as error:
        settings.passwords(INSTITUTIONS)
    assert "19/12/83" not in str(error.value)


def test_details_and_passwords_are_not_printed() -> None:
    settings = Settings(
        identity={"national_id": NATIONAL_ID, "birth_date": "2000-01-02"},
        pdf_passwords={"esun": "abc"},
    )
    for text in (repr(settings), repr(settings.passwords(INSTITUTIONS))):
        assert NATIONAL_ID not in text
        assert "2000" not in text
        assert "abc" not in text


def test_an_env_file_is_read_only_when_one_is_given(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    # Where the settings used to look by default, relative to the working directory.
    env_file = tmp_path / ".local" / "parser.env"
    env_file.parent.mkdir()
    env_file.write_text(f"PARSER_IDENTITY__NATIONAL_ID={NATIONAL_ID}\n")
    working = tmp_path / "apps" / "parser"
    working.mkdir(parents=True)
    monkeypatch.chdir(working)

    assert Settings().passwords(INSTITUTIONS).identity.national_id == ""
    assert Settings.load(env_file).passwords(INSTITUTIONS).identity.national_id == NATIONAL_ID
    assert Settings.load(tmp_path / "missing.env").passwords(INSTITUTIONS).identity.national_id == ""


def test_variables_are_named_after_the_settings_fields() -> None:
    assert variable("ocr") == "PARSER_OCR"
    assert identity_variable("national_id") == "PARSER_IDENTITY__NATIONAL_ID"
    assert chosen_password_variable("taishin") == "PARSER_PDF_PASSWORDS__TAISHIN"
    assert set(Settings.model_fields) >= {"ocr", "identity", "pdf_passwords"}
