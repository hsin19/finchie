"""Where the backend is and how to get through Cloudflare Access to it.

Read from FINCHIE_* environment variables, then from a TOML file in the config directory
($XDG_CONFIG_HOME/finchie/config.toml, by default ~/.config/finchie/config.toml). Nothing is written.
"""

import stat
import tomllib
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

SETUP = """\
Set where the backend is, in {path}:

    url = "https://finchie.example.com"
    # The Access service token for agents (the backend's BACKEND_ACCESS_AGENT_CLIENT_ID); leave both
    # out for a backend without Access, such as one running locally.
    client_id = "….access"
    client_secret = "…"

or in FINCHIE_URL, FINCHIE_CLIENT_ID, and FINCHIE_CLIENT_SECRET, which take precedence."""


class ConfigError(Exception):
    pass


@dataclass(frozen=True)
class Settings:
    url: str
    client_id: str | None = None
    client_secret: str | None = None
    # A reason to worry about the file the settings came from, for the caller to pass on.
    warning: str | None = None


def path(environ: Mapping[str, str], home: Path) -> Path:
    base = Path(environ["XDG_CONFIG_HOME"]) if environ.get("XDG_CONFIG_HOME") else home / ".config"
    return base / "finchie" / "config.toml"


def load(environ: Mapping[str, str], home: Path) -> Settings:
    file = path(environ, home)
    values: dict[str, object] = {}
    warning = None
    if file.exists():
        try:
            values = tomllib.loads(file.read_text())
        except (OSError, tomllib.TOMLDecodeError) as e:
            raise ConfigError(f"cannot read {file}: {e}") from e
        if "client_secret" in values and file.stat().st_mode & (stat.S_IRWXG | stat.S_IRWXO):
            warning = f"{file} holds the service token's secret and others can read it: chmod 600 {file}"

    def setting(key: str) -> str | None:
        value = environ.get(f"FINCHIE_{key.upper()}") or values.get(key)
        if value is not None and not isinstance(value, str):
            raise ConfigError(f"{file}: {key} must be a string")
        return value or None

    url = setting("url")
    if url is None:
        raise ConfigError(SETUP.format(path=file))
    client_id, client_secret = setting("client_id"), setting("client_secret")
    # Half a token is a mistake, not a backend without Access.
    if (client_id is None) != (client_secret is None):
        raise ConfigError("set both client_id and client_secret (FINCHIE_CLIENT_ID, FINCHIE_CLIENT_SECRET), or neither")
    return Settings(url=url.rstrip("/"), client_id=client_id, client_secret=client_secret, warning=warning)
