import re
from pathlib import Path
from typing import Any

import pytest
import yaml

from finchie_cli import help
from finchie_cli.contract import Contract, kebab
from finchie_cli.main import _parser

BACKEND_CONTRACT = Path(__file__).parents[2] / "backend" / "api" / "openapi.yaml"


@pytest.fixture(scope="module")
def real() -> Contract:
    document: dict[str, Any] = yaml.safe_load(BACKEND_CONTRACT.read_text())
    return Contract(document)


@pytest.mark.parametrize(
    ("name", "command"),
    [
        ("searchTransactions", "search-transactions"),
        ("getOpenAPI", "get-open-api"),
        ("getDocumentPreviewPdf", "get-document-preview-pdf"),
        ("fillId", "fill-id"),
        ("q", "q"),
    ],
)
def test_names_become_kebab_case(name: str, command: str) -> None:
    assert kebab(name) == command


def test_every_command_of_the_real_contract_builds_and_has_its_help(real: Contract) -> None:
    commands = [op.command for op in real.operations]
    assert len(commands) == len(set(commands))
    assert "search-transactions" in commands
    for op in real.operations:
        _parser(real, op)
        assert help.operation(real, op).startswith(f"finchie {op.command}")
    assert "search-transactions" in help.overview(real, "https://finchie.example")


def test_every_operation_of_the_real_contract_has_a_summary_for_the_command_list(real: Contract) -> None:
    assert [op.id for op in real.operations if not op.summary] == []


def test_pages_where_the_real_contract_pages(real: Contract) -> None:
    paging = {op.command: op.paging for op in real.operations}
    assert paging["search-transactions"] == "offset"
    assert paging["list-account-lines"] == "offset"
    assert paging["list-documents"] == "before"
    assert paging["list-statements"] is None
    assert paging["get-net-worth"] is None


def test_the_search_help_carries_what_its_totals_mean(real: Contract) -> None:
    op = real.find("search-transactions")
    assert op is not None
    text = help.operation(real, op)
    assert "--hide-transfers, --no-hide-transfers" in text
    # Decimal is spelled out where it first appears, in the rows, and named after that.
    assert "amount: Decimal (string)" in text
    assert "sum: Decimal — Amounts as printed" in text


SKILL = Path(__file__).parents[3] / ".agents" / "skills" / "finchie" / "SKILL.md"


def test_the_skill_names_only_commands_and_options_the_contract_has(real: Contract) -> None:
    """The skill teaches agents the CLI: a command renamed in openapi.yaml must not leave it stale."""
    text = SKILL.read_text()
    code = re.findall(r"```\w*\n(.*?)```", text, re.DOTALL)
    code += re.findall(r"`([^`\n]+)`", re.sub(r"```.*?```", "", text, flags=re.DOTALL))
    commands, options = set(), set()
    for span in code:
        commands.update(re.findall(r"finchie ([a-z][a-z-]*)", span))
        if re.fullmatch(r"[a-z]+(?:-[a-z]+)+", span):
            commands.add(span)
        options.update(re.findall(r"(?<![\w-])--[a-z][a-z-]*", span))
    assert {"search-transactions", "get-net-worth"} <= commands
    assert "--hide-transfers" in options

    offered = {"--help"} | {o for op in real.operations for o in _parser(real, op)._option_string_actions}
    assert sorted(c for c in commands if real.find(c) is None) == []
    assert sorted(options - offered) == []
