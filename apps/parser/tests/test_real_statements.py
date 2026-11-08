"""Regression against real statements kept outside the repository (ADR 0006).

Set FINCHIE_REAL_STATEMENTS_DIR to a folder of .eml files (conventionally `.local/statements/`) and
PARSER_IDENTITY__* for the details the banks' password rules use (ADR 0014). Skipped when the folder is
not set.
"""

import os
from pathlib import Path

import pytest

from finchie_parser.model import Kind
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
    reconciliation = statement.reconciliation

    purchases = [tx for tx in statement.transactions if tx.kind is Kind.PURCHASE]
    print(
        f"\n{path.name}: {result.parser} closing={statement.closing_date} due={statement.due_date} "
        f"transactions={len(statement.transactions)} computed={reconciliation.computed} "
        f"stated={reconciliation.stated} total_due={statement.total_due} warnings={len(result.warnings)}"
    )
    assert reconciliation.balanced, f"{reconciliation.computed} != {reconciliation.stated}"
    assert purchases, "a statement with no purchases is suspicious"
    assert all(tx.description for tx in statement.transactions), "every transaction needs a description"
    assert not result.warnings
