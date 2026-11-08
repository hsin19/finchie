"""PDF passwords: the owner's details, and the order a parser tries its passwords in (ADR 0014)."""

from collections.abc import Callable, Iterator, Mapping
from dataclasses import dataclass, field
from datetime import date


class MissingDetailError(Exception):
    """A bank's password rule needs a detail the deployment has not set."""

    def __init__(self, variable: str) -> None:
        super().__init__(f"needs {variable}")
        self.variable = variable


@dataclass(frozen=True, repr=False)
class Identity:
    """The details banks build statement passwords from. Never logged: repr shows only which are set."""

    national_id: str = ""
    birth_date: date | None = None

    def require_national_id(self) -> str:
        if not self.national_id:
            raise MissingDetailError("PARSER_IDENTITY__NATIONAL_ID")
        return self.national_id

    def require_birth_date(self) -> date:
        if self.birth_date is None:
            raise MissingDetailError("PARSER_IDENTITY__BIRTH_DATE")
        return self.birth_date

    def __repr__(self) -> str:
        return f"Identity(national_id={bool(self.national_id)}, birth_date={self.birth_date is not None})"


Rule = Callable[[Identity], list[str]]


@dataclass(frozen=True, repr=False)
class Passwords:
    identity: Identity = field(default_factory=Identity)
    # Institution ID to a password the owner chose in place of the bank's rule.
    chosen: Mapping[str, str] = field(default_factory=dict)

    def candidates(self, institution: str, rule: Rule) -> Iterator[str]:
        """No password, then the one the owner chose, then the bank's rule.

        Lazy: the rule runs, and may raise MissingDetailError, only when the others did not open the PDF.
        The rule still runs after a chosen password, since statements from before the change use it.
        """
        yield ""
        if chosen := self.chosen.get(institution):
            yield chosen
        yield from rule(self.identity)

    def __repr__(self) -> str:
        return f"Passwords({self.identity!r}, chosen={sorted(self.chosen)})"
