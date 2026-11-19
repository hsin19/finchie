from pathlib import Path

import pytest

from finchie_cli.config import ConfigError, Settings, load


def write_config(home: Path, text: str, mode: int = 0o600) -> Path:
    file = home / ".config" / "finchie" / "config.toml"
    file.parent.mkdir(parents=True)
    file.write_text(text)
    file.chmod(mode)
    return file


def test_reads_the_file_in_the_config_directory(tmp_path: Path) -> None:
    write_config(tmp_path, 'url = "https://finchie.example/"\nclient_id = "a.access"\nclient_secret = "s"\n')
    assert load({}, tmp_path) == Settings(url="https://finchie.example", client_id="a.access", client_secret="s")


def test_environment_variables_take_precedence_over_the_file(tmp_path: Path) -> None:
    write_config(tmp_path, 'url = "https://finchie.example"\n')
    settings = load({"FINCHIE_URL": "http://localhost:8080"}, tmp_path)
    assert settings.url == "http://localhost:8080"


def test_follows_xdg_config_home(tmp_path: Path) -> None:
    file = tmp_path / "elsewhere" / "finchie" / "config.toml"
    file.parent.mkdir(parents=True)
    file.write_text('url = "https://finchie.example"\n')
    assert load({"XDG_CONFIG_HOME": str(tmp_path / "elsewhere")}, tmp_path / "home").url == "https://finchie.example"


def test_without_a_url_says_where_to_set_one(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match=r"\.config/finchie/config\.toml"):
        load({}, tmp_path)


def test_half_a_service_token_is_a_mistake(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="set both client_id and client_secret"):
        load({"FINCHIE_URL": "https://finchie.example", "FINCHIE_CLIENT_ID": "a.access"}, tmp_path)


def test_warns_while_others_can_read_the_secret(tmp_path: Path) -> None:
    file = write_config(tmp_path, 'url = "u"\nclient_id = "a.access"\nclient_secret = "s"\n', mode=0o644)
    warning = load({}, tmp_path).warning
    assert warning is not None
    assert "chmod 600" in warning
    file.chmod(0o600)
    assert load({}, tmp_path).warning is None
