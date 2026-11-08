"""Regression against real statements kept outside the repository (ADR 0006).

Set FINCHIE_REAL_STATEMENTS_DIR to a folder of .eml files (conventionally `.local/statements/`) and
PARSER_IDENTITY__* for the details the banks' password rules use (ADR 0014). Skipped when the folder is
not set.
"""

import os
from pathlib import Path

import pytest

from finchie_parser.model import DepositStatement, Kind, SecuritiesStatement, Statement
from finchie_parser.ocr import RapidOcr
from finchie_parser.pipeline import parse_document
from finchie_parser.settings import Settings

_DIR = os.environ.get("FINCHIE_REAL_STATEMENTS_DIR", "")
_FILES = sorted(Path(_DIR).glob("*.eml")) if _DIR else []
_OCR = RapidOcr()

pytestmark = pytest.mark.skipif(not _DIR, reason="FINCHIE_REAL_STATEMENTS_DIR is not set")


@pytest.mark.parametrize("path", _FILES, ids=[p.name for p in _FILES])
def test_real_statement_reconciles(path: Path) -> None:
    result = parse_document(path.read_bytes(), passwords=Settings().passwords(), ocr=_OCR)
    statement = result.statement
    if isinstance(statement, DepositStatement):
        _check_deposit(path, result.parser, statement)
    elif isinstance(statement, SecuritiesStatement):
        _check_securities(path, result.parser, statement)
    else:
        _check_bill(path, result.parser, statement)
    assert not result.warnings


def _check_bill(path: Path, parser: str, statement: Statement) -> None:
    reconciliation = statement.reconciliation
    purchases = [tx for tx in statement.transactions if tx.kind is Kind.PURCHASE]
    print(
        f"\n{path.name}: {parser} closing={statement.closing_date} due={statement.due_date} "
        f"transactions={len(statement.transactions)} computed={reconciliation.computed} "
        f"stated={reconciliation.stated} total_due={statement.total_due}"
    )
    assert reconciliation.balanced, f"{reconciliation.computed} != {reconciliation.stated}"
    assert purchases, "a statement with no purchases is suspicious"
    assert all(tx.description for tx in statement.transactions), "every transaction needs a description"


def _check_deposit(path: Path, parser: str, statement: DepositStatement) -> None:
    # No amounts printed: deposit balances say more about the owner than a bill's totals.
    lines = [line for account in statement.accounts for line in account.lines]
    print(
        f"\n{path.name}: {parser} {statement.period_start}..{statement.period_end} "
        f"accounts={len(statement.accounts)} lines={len(lines)}"
    )
    for account in statement.accounts:
        reconciliation = account.reconciliation
        assert reconciliation is None or reconciliation.balanced, f"{account.name}: {reconciliation}"
    assert statement.accounts, "a statement with no accounts is suspicious"
    assert all(line.description for line in lines), "every line needs a description"


def _check_securities(path: Path, parser: str, statement: SecuritiesStatement) -> None:
    # No amounts or holdings printed, as for deposits.
    print(
        f"\n{path.name}: {parser} {statement.period_start}..{statement.period_end} "
        f"holdings={len(statement.holdings)} trades={len(statement.trades)}"
    )
    reconciliation = statement.reconciliation
    assert reconciliation.balanced, (
        f"{reconciliation.stated_label}: {reconciliation.computed} != {reconciliation.stated}"
    )
    assert statement.holdings or statement.trades, "a statement with neither holdings nor trades is suspicious"
    assert all(holding.name for holding in statement.holdings), "every holding needs a name"
