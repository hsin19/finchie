"""The deployment's settings, from `PARSER_*` environment variables. The names of those variables are
known here only: other modules ask `variable` for the one to name in a message."""

from collections.abc import Collection
from datetime import date
from pathlib import Path

from pydantic import BaseModel, Field, SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict

from finchie_parser.passwords import Identity, Passwords

_PREFIX = "PARSER_"
_DELIMITER = "__"


def variable(*path: str) -> str:
    """The environment variable of a setting, by its path: ("identity", "national_id") ->
    PARSER_IDENTITY__NATIONAL_ID."""
    return _PREFIX + _DELIMITER.join(part.upper() for part in path)


def identity_variable(detail: str) -> str:
    """The variable that holds one of the owner's details (an Identity field)."""
    return variable("identity", detail)


def chosen_password_variable(institution: str) -> str:
    """The variable for a password the owner chose at a bank, in place of its rule."""
    return variable("pdf_passwords", institution)


class IdentitySettings(BaseModel):
    """The owner's details the banks' password rules use (ADR 0014). Set only those a rule needs."""

    national_id: SecretStr | None = None
    # YYYY-MM-DD; each rule formats it as its bank asks.
    birth_date: SecretStr | None = None


class Settings(BaseSettings):
    """Read from `PARSER_*` environment variables, nested keys split by `__`.

    Only the environment: an env file is read only when its path is given (`Settings.load`), so that
    tests and the image never pick up a developer's `.local/parser.env` (ADR 0006).
    """

    model_config = SettingsConfigDict(
        env_prefix=_PREFIX,
        env_nested_delimiter=_DELIMITER,
        env_file=None,
        extra="ignore",
    )

    host: str = "[::]"
    port: int = 50051
    # One thread pool serves ParseDocument, OpenDocument, and health checks. It must exceed the backend's
    # BACKEND_PARSE_CONCURRENCY (2): parses that fill it starve previews and the 3-second healthcheck.
    workers: int = 4
    # Must exceed the backend's BACKEND_MAX_UPLOAD_MB (32): a request carries the whole file plus its
    # other fields. The backend allows itself the same 1 MiB of headroom when sending.
    max_message_mb: int = 33
    ocr: bool = True
    log_level: str = "INFO"
    # The release, reported with every trace, metric, and log; the image sets it (release.yml passes the
    # tag's version). Not to be confused with a bank parser's version.
    version: str = "dev"
    # A secret the caller sends with every call (`authorization: Bearer ...`); unset, any caller that reaches
    # the port is served. The backend's BACKEND_PARSER_TOKEN, the same value.
    token: SecretStr | None = None
    # PARSER_IDENTITY__NATIONAL_ID=... ; see IdentitySettings.
    identity: IdentitySettings = Field(default_factory=IdentitySettings)
    # PARSER_PDF_PASSWORDS__TAISHIN=... ; keys are institution IDs. Only for a bank whose password the
    # owner changed: the bank's rule covers the rest.
    pdf_passwords: dict[str, SecretStr] = Field(default_factory=dict)

    @classmethod
    def load(cls, env_file: Path | None = None) -> "Settings":
        """From the environment, and from `env_file` too when given and present."""
        if env_file is not None and env_file.is_file():
            return cls(_env_file=env_file)
        return cls()

    def passwords(self, institutions: Collection[str]) -> Passwords:
        """The owner's details and chosen passwords. A chosen password for an institution no parser has
        is a typo that would otherwise go unnoticed, so it fails."""
        if unknown := sorted(set(self.pdf_passwords) - set(institutions)):
            names = ", ".join(chosen_password_variable(institution) for institution in unknown)
            raise ValueError(f"{names}: no parser has that institution; known are {sorted(institutions)}")
        national_id = self.identity.national_id
        return Passwords(
            Identity(
                national_id=national_id.get_secret_value() if national_id else "",
                birth_date=_birth_date(self.identity.birth_date),
            ),
            {institution: secret.get_secret_value() for institution, secret in self.pdf_passwords.items()},
        )


def _birth_date(secret: SecretStr | None) -> date | None:
    if secret is None:
        return None
    try:
        return date.fromisoformat(secret.get_secret_value())
    except ValueError:
        # Without the value: the message would print it.
        raise ValueError(f"{identity_variable('birth_date')} must be YYYY-MM-DD") from None
