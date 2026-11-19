import io
import json
import threading
from collections.abc import Callable, Iterator
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qsl, urlsplit

import pytest

from finchie_cli.main import main

# A contract small enough to know by heart, with each kind of thing the real one has.
CONTRACT = {
    "openapi": "3.0.3",
    "paths": {
        "/api/things": {
            "get": {
                "operationId": "listThings",
                "summary": "Things, a page at a time",
                "parameters": [
                    {"$ref": "#/components/parameters/Limit"},
                    {"name": "offset", "in": "query", "schema": {"type": "integer"}},
                    {
                        "name": "kind",
                        "in": "query",
                        "style": "form",
                        "explode": True,
                        "schema": {"type": "array", "items": {"$ref": "#/components/schemas/Kind"}},
                    },
                    {"name": "open", "in": "query", "description": "Only open ones", "schema": {"type": "boolean"}},
                ],
                "responses": {
                    "200": {
                        "description": "A page",
                        "content": {
                            "application/json": {
                                "schema": {"type": "array", "items": {"$ref": "#/components/schemas/Thing"}}
                            }
                        },
                    }
                },
            },
            "post": {
                "operationId": "createThing",
                "summary": "A thing of the owner's",
                "requestBody": {
                    "required": True,
                    "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}},
                },
                "responses": {"204": {"description": "Created"}},
            },
        },
        "/api/things/{id}": {
            "get": {
                "operationId": "getThing",
                "summary": "One thing",
                "parameters": [{"name": "id", "in": "path", "required": True, "schema": {"type": "integer"}}],
                "responses": {
                    "200": {
                        "description": "The thing",
                        "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}},
                    },
                    "404": {"$ref": "#/components/responses/Problem"},
                },
            }
        },
        "/api/things/{id}/file": {
            "get": {
                "operationId": "getThingPdf",
                "summary": "Its PDF",
                "parameters": [{"name": "id", "in": "path", "required": True, "schema": {"type": "integer"}}],
                "responses": {
                    "200": {
                        "description": "The PDF",
                        "content": {"application/pdf": {"schema": {"type": "string", "format": "binary"}}},
                    }
                },
            }
        },
        "/api/docs": {
            "get": {
                "operationId": "listDocs",
                "summary": "Documents, newest first",
                "parameters": [
                    {"$ref": "#/components/parameters/Limit"},
                    {"name": "before", "in": "query", "schema": {"type": "integer"}},
                ],
                "responses": {
                    "200": {
                        "description": "A page",
                        "content": {
                            "application/json": {
                                "schema": {
                                    "type": "object",
                                    "properties": {
                                        "items": {"type": "array", "items": {"$ref": "#/components/schemas/Thing"}}
                                    },
                                }
                            }
                        },
                    }
                },
            }
        },
    },
    "components": {
        "parameters": {
            "Limit": {
                "name": "limit",
                "in": "query",
                "schema": {"type": "integer", "minimum": 1, "maximum": 2, "default": 2},
            }
        },
        "schemas": {
            "Kind": {"type": "string", "enum": ["a", "b"], "description": "a: one kind. b: the other"},
            "Decimal": {"type": "string", "description": "Decimal number as a string"},
            "Thing": {
                "type": "object",
                "required": ["id", "amount"],
                "properties": {
                    "id": {"type": "integer"},
                    "amount": {"allOf": [{"$ref": "#/components/schemas/Decimal"}], "description": "What it cost"},
                    "kind": {"$ref": "#/components/schemas/Kind"},
                    "note": {"type": "string", "nullable": True},
                    "parts": {"type": "array", "items": {"$ref": "#/components/schemas/Thing"}},
                },
            },
        },
        "responses": {
            "Problem": {
                "description": "Error",
                "content": {
                    "application/json": {"schema": {"type": "object", "properties": {"message": {"type": "string"}}}}
                },
            }
        },
    },
}


@dataclass(frozen=True)
class Request:
    method: str
    path: str
    query: list[tuple[str, str]]
    headers: dict[str, str]
    body: bytes


@dataclass(frozen=True)
class Answer:
    status: int = 200
    content_type: str = "application/json"
    body: bytes = b""
    headers: dict[str, str] = field(default_factory=dict)


def json_answer(value: object, status: int = 200) -> Answer:
    return Answer(status=status, body=json.dumps(value).encode())


type Route = Callable[[Request], Answer]


@dataclass
class FakeAPI:
    url: str
    routes: dict[tuple[str, str], Route]
    requests: list[Request]

    def calls(self) -> list[Request]:
        """The requests other than the contract's."""
        return [r for r in self.requests if r.path != "/api/openapi.json"]


@pytest.fixture
def api() -> Iterator[FakeAPI]:
    routes: dict[tuple[str, str], Route] = {("GET", "/api/openapi.json"): lambda _: json_answer(CONTRACT)}
    requests: list[Request] = []

    class Handler(BaseHTTPRequestHandler):
        def _answer(self) -> None:
            url = urlsplit(self.path)
            length = int(self.headers.get("Content-Length") or 0)
            request = Request(
                method=self.command,
                path=url.path,
                query=parse_qsl(url.query),
                # HTTP header names are case-insensitive; urllib sends them capitalized.
                headers={k.lower(): v for k, v in self.headers.items()},
                body=self.rfile.read(length),
            )
            requests.append(request)
            route = routes.get((self.command, url.path))
            answer = route(request) if route else json_answer({"message": "no such route"}, 404)
            self.send_response(answer.status)
            self.send_header("Content-Type", answer.content_type)
            self.send_header("Content-Length", str(len(answer.body)))
            for name, value in answer.headers.items():
                self.send_header(name, value)
            self.end_headers()
            self.wfile.write(answer.body)

        # http.server's names for the handler of each method.
        do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = _answer  # noqa: N815

        def log_message(self, format: str, *args: object) -> None:
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    try:
        yield FakeAPI(url=f"http://127.0.0.1:{server.server_port}", routes=routes, requests=requests)
    finally:
        server.shutdown()
        server.server_close()


@dataclass(frozen=True)
class Run:
    code: int
    stdout: bytes
    stderr: str

    @property
    def text(self) -> str:
        return self.stdout.decode()


type Cli = Callable[..., Run]


@pytest.fixture
def cli(api: FakeAPI, tmp_path: Path) -> Cli:
    def run(*argv: str, stdin: bytes = b"", tty: bool = False, env: dict[str, str] | None = None) -> Run:
        environ = {"FINCHIE_URL": api.url, **(env or {})}
        stdout, stderr = io.BytesIO(), io.StringIO()
        code = main(list(argv), environ, tmp_path, io.BytesIO(stdin), stdout, stderr, tty=tty)
        return Run(code, stdout.getvalue(), stderr.getvalue())

    return run
