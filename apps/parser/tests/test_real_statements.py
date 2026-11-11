"""Regression against real statements kept outside the repository (ADR 0006).

Set FINCHIE_REAL_STATEMENTS_DIR to a folder of .eml files (conventionally `.local/statements/`, which
`task parser:test-real` does) and the details the banks' password rules use (ADR 0014), as PARSER_IDENTITY__*
or in `.local/parser.env`, which these tests read. Skipped when the folder is not set; with it set, a folder
without e-mails, or without one for each parser, fails rather than pass having checked nothing.
"""

import os
from collections import Counter
from functools import cache
from pathlib import Path

import pytest

from finchie_parser.documents import read_mail
from finchie_parser.model import AccountKind, LineKind
from finchie_parser.ocr import RapidOcr
from finchie_parser.parsers import PARSERS
from finchie_parser.parsers.base import institutions
from finchie_parser.passwords import Passwords
from finchie_parser.pipeline import parse_document
from finchie_parser.selection import claimants
from finchie_parser.settings import Settings

_DIR = os.environ.get("FINCHIE_REAL_STATEMENTS_DIR", "")
_FILES = sorted(Path(_DIR).glob("*.eml")) if _DIR else []
_ENV_FILE = Path(__file__).resolve().parents[3] / ".local" / "parser.env"
_OCR = RapidOcr()

pytestmark = [
    pytest.mark.skipif(not _DIR, reason="FINCHIE_REAL_STATEMENTS_DIR is not set"),
    pytest.mark.real_environment,
]


@cache
def _passwords() -> Passwords:
    return Settings.load(_ENV_FILE).passwords(institutions(PARSERS))


def test_the_folder_has_statements() -> None:
    assert _FILES, f"no .eml in {_DIR}"


def test_every_parser_has_a_statement_to_check() -> None:
    claimed = {parser.name for path in _FILES for parser in claimants(read_mail(path.read_bytes()), PARSERS)}
    assert sorted(parser.name for parser in PARSERS if parser.name not in claimed) == []


@pytest.mark.parametrize("path", _FILES, ids=[p.name for p in _FILES])
def test_real_statement_balances(path: Path) -> None:
    result = parse_document(path.read_bytes(), passwords=_passwords(), ocr=_OCR)
    statement = result.statement
    kinds = Counter(account.kind for account in statement.accounts)
    # No amounts printed: balances say more about the owner than counts do.
    print(
        f"\n{path.name}: {result.parser} {statement.period_start}..{statement.closing_date} "
        f"accounts={dict(kinds)} lines={statement.line_count} checks={len(statement.checks)}"
    )
    unbalanced = [check.name for check in statement.checks if not check.balanced]
    assert unbalanced == [], f"checks that do not balance: {unbalanced}"
    assert not result.warnings
    lines = [line for account in statement.accounts for line in account.lines]
    assert all(line.description for line in lines), "every line needs a description"
    assert all(balance.unit for account in statement.accounts for balance in account.balances)
    if statement.card_bill is not None:
        assert kinds[AccountKind.CREDIT_CARD] == 1
        card = [line for line in lines if line.kind is LineKind.PURCHASE]
        assert card, "a bill with no purchases is suspicious"
        assert statement.checks, "a bill has its new charges to check"
    elif kinds[AccountKind.SECURITIES]:
        [brokerage] = statement.accounts
        assert brokerage.balances or brokerage.lines, "a statement with neither holdings nor trades is suspicious"
    else:
        assert set(kinds) == {AccountKind.DEPOSIT}, "a statement with no accounts is suspicious"
        numbers = [account.number for account in statement.accounts]
        assert len(numbers) == len(set(numbers)), "an account reported twice"
