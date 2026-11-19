"""`finchie <command> [options]`: one API operation, its answer on stdout."""

import argparse
import json
import os
import sys
import urllib.parse
from collections.abc import Iterator, Mapping, Sequence
from pathlib import Path
from typing import IO, Any, NoReturn

from finchie_cli import config, help
from finchie_cli.client import ApiError, CallError, Client, Reply
from finchie_cli.contract import Contract, Operation, Parameter, scalar

CONTRACT_PATH = "/api/openapi.json"

# Exit statuses: the API or the network failed, or the command line is wrong.
FAILED, USAGE = 1, 2


class UsageError(Exception):
    pass


class _Parser(argparse.ArgumentParser):
    def error(self, message: str) -> NoReturn:
        # The message alone: the usage argparse would print is a wall of every option.
        raise UsageError(f"{self.prog}: {message} (see {self.prog} --help)")


def run() -> None:
    try:
        code = main(sys.argv[1:], os.environ, Path.home(), sys.stdin.buffer, sys.stdout.buffer, sys.stderr)
        sys.stdout.flush()
    except BrokenPipeError:
        # A reader that stopped early (`--all | head`) is no error. Python would print a traceback, and
        # again at exit flushing stdout, unless stdout points elsewhere.
        os.dup2(os.open(os.devnull, os.O_WRONLY), sys.stdout.fileno())
        code = 0
    sys.exit(code)


def main(
    argv: Sequence[str],
    environ: Mapping[str, str],
    home: Path,
    stdin: IO[bytes],
    stdout: IO[bytes],
    stderr: IO[str],
    tty: bool | None = None,
) -> int:
    if tty is None:
        tty = stdout.isatty()
    try:
        settings = config.load(environ, home)
    except config.ConfigError as e:
        print(e, file=stderr)
        return USAGE
    if settings.warning:
        print(f"warning: {settings.warning}", file=stderr)
    client = Client(settings)
    try:
        contract = _contract(client, settings.url)
        if not argv or argv[0] in ("-h", "--help", "help"):
            stdout.write(help.overview(contract, settings.url).encode())
            return 0
        op = contract.find(argv[0])
        if op is None:
            raise UsageError(f"finchie: no command {argv[0]!r} (see finchie --help)")
        rest = argv[1:]
        if "-h" in rest or "--help" in rest:
            stdout.write(help.operation(contract, op).encode())
            return 0
        _call(client, contract, op, rest, stdin, stdout, tty)
        return 0
    except UsageError as e:
        print(e, file=stderr)
        return USAGE
    except ApiError as e:
        print(f"HTTP {e.reply.status}: {e.reply.body.decode(errors='replace').strip()}", file=stderr)
        return FAILED
    except CallError as e:
        print(e, file=stderr)
        return FAILED


def _contract(client: Client, url: str) -> Contract:
    older = f"{url}{CONTRACT_PATH} is not the API's contract: is the backend older than the CLI?"
    try:
        reply = client.send("GET", CONTRACT_PATH)
    except ApiError as e:
        if e.reply.status == 404:
            raise CallError(older) from e
        raise
    try:
        if not reply.is_json:
            raise ValueError(reply.content_type)
        return Contract(json.loads(reply.body))
    except ValueError as e:
        raise CallError(older) from e


def _call(
    client: Client,
    contract: Contract,
    op: Operation,
    argv: Sequence[str],
    stdin: IO[bytes],
    stdout: IO[bytes],
    tty: bool,
) -> None:
    if op.body is not None and not op.body.is_json:
        raise UsageError(f"finchie {op.command}: takes a {op.body.media_type} body, which this CLI does not send")
    args = _parser(contract, op).parse_args(argv)
    path = op.path
    query: dict[str, list[str]] = {}
    for p in op.parameters:
        value = getattr(args, p.dest)
        if value is None:
            continue
        if p.location == "path":
            path = path.replace("{" + p.name + "}", urllib.parse.quote(str(value), safe=""))
        else:
            query[p.name] = [scalar(v) for v in (value if isinstance(value, list) else [value])]
    body = _body(args.body, stdin) if op.body is not None and args.body is not None else None

    if op.paging is not None and args.all:
        for item in _every_item(client, contract, op, path, query):
            stdout.write(json.dumps(item, ensure_ascii=False, separators=(",", ":")).encode() + b"\n")
        return
    reply = client.send(op.method, path, query, body)
    _write(reply, stdout, tty)


def _parser(contract: Contract, op: Operation) -> _Parser:
    parser = _Parser(prog=f"finchie {op.command}", add_help=False, allow_abbrev=False)
    for p in op.parameters:
        if p.location == "path":
            parser.add_argument(p.dest, metavar=p.name, type=_converter(contract, p.schema))
        elif p.location == "query":
            parser.add_argument(p.option, dest=p.dest, required=p.required, **_kind(contract, p))
    if op.body is not None:
        parser.add_argument("--body", required=op.body_required)
    if op.paging is not None:
        parser.add_argument("--all", action="store_true")
    return parser


def _kind(contract: Contract, p: Parameter) -> dict[str, Any]:
    schema = contract.resolve(p.schema)
    if schema.get("type") == "boolean":
        return {"action": argparse.BooleanOptionalAction, "default": None}
    if schema.get("type") == "array":
        items = contract.resolve(schema.get("items", {}))
        return {"action": "append", "type": _converter(contract, items), "choices": items.get("enum")}
    return {"type": _converter(contract, schema), "choices": schema.get("enum")}


def _converter(contract: Contract, schema: dict[str, Any]) -> type[int] | type[str]:
    return int if contract.resolve(schema).get("type") == "integer" else str


def _body(text: str, stdin: IO[bytes]) -> bytes:
    if text == "-":
        raw = stdin.read().decode()
    elif text.startswith("@"):
        try:
            raw = Path(text[1:]).read_text()
        except OSError as e:
            raise UsageError(f"--body: cannot read {text[1:]}: {e}") from e
    else:
        raw = text
    try:
        return json.dumps(json.loads(raw), ensure_ascii=False).encode()
    except json.JSONDecodeError as e:
        raise UsageError(f"--body is not JSON: {e}") from e


def _every_item(
    client: Client, contract: Contract, op: Operation, path: str, query: dict[str, list[str]]
) -> Iterator[Any]:
    limit_parameter = op.parameter("limit")
    assert limit_parameter is not None
    if "limit" in query:
        limit = int(query["limit"][0])
    else:
        schema = contract.resolve(limit_parameter.schema)
        limit = int(schema.get("maximum") or schema.get("default") or 100)
        query["limit"] = [str(limit)]
    if limit < 1:
        raise UsageError(f"finchie {op.command}: --all needs a --limit of at least 1")
    while True:
        reply = client.send(op.method, path, query)
        page = json.loads(reply.body)
        items = page if isinstance(page, list) else page.get("items") if isinstance(page, dict) else None
        if not isinstance(items, list):
            raise CallError(f"finchie {op.command}: a page holds no list of items to go through")
        yield from items
        if len(items) < limit:
            return
        if op.paging == "offset":
            query["offset"] = [str(int(query.get("offset", ["0"])[0]) + len(items))]
        else:
            query["before"] = [str(items[-1]["id"])]


def _write(reply: Reply, stdout: IO[bytes], tty: bool) -> None:
    if not reply.body:
        return
    if reply.is_json:
        value = json.loads(reply.body)
        if tty:
            text = json.dumps(value, ensure_ascii=False, indent=2)
        else:
            text = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
        stdout.write(text.encode() + b"\n")
        return
    media = reply.content_type.split(";", 1)[0].strip()
    if tty and not media.startswith("text/") and not media.endswith("xml"):
        raise CallError(f"the answer is {media}, {len(reply.body)} bytes: redirect stdout to a file")
    stdout.write(reply.body)
