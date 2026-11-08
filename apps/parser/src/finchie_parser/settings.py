from datetime import date

from pydantic import BaseModel, Field, SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict

from finchie_parser.passwords import Identity, Passwords


class IdentitySettings(BaseModel):
    """The owner's details the banks' password rules use (ADR 0014). Set only those a rule needs."""

    national_id: SecretStr | None = None
    # YYYY-MM-DD; each rule formats it as its bank asks.
    birth_date: SecretStr | None = None


class Settings(BaseSettings):
    """Read from `PARSER_*` environment variables, nested keys split by `__`.

    For local runs, `.local/parser.env` at the repository root is read too
    (gitignored, ADR 0006).
    """

    model_config = SettingsConfigDict(
        env_prefix="PARSER_",
        env_nested_delimiter="__",
        env_file="../../.local/parser.env",
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
    # PARSER_IDENTITY__NATIONAL_ID=... ; see IdentitySettings.
    identity: IdentitySettings = Field(default_factory=IdentitySettings)
    # PARSER_PDF_PASSWORDS__TAISHIN=... ; keys are institution IDs. Only for a bank whose password the
    # owner changed: the bank's rule covers the rest.
    pdf_passwords: dict[str, SecretStr] = Field(default_factory=dict)

    def passwords(self) -> Passwords:
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
        raise ValueError("PARSER_IDENTITY__BIRTH_DATE must be YYYY-MM-DD") from None
