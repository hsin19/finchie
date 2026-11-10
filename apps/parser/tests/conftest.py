"""Keeps the tests hermetic: no PARSER_* variable from the shell reaches them, unless a test is marked
`real_environment` (the real-statement regression, ADR 0006)."""

import os

import pytest


@pytest.fixture(autouse=True)
def _no_parser_environment(request: pytest.FixtureRequest, monkeypatch: pytest.MonkeyPatch) -> None:
    if request.node.get_closest_marker("real_environment") is None:
        for name in [name for name in os.environ if name.startswith("PARSER_")]:
            monkeypatch.delenv(name)
