"""The API's contract, read from the backend (GET /api/openapi.json), as the commands it offers.

Every operation is a command named after its operationId in kebab case (searchTransactions is
search-transactions), and its parameters are options named the same way. The contract comes from the
deployment at every run, so the commands are always the ones it serves.
"""

import re
from dataclasses import dataclass
from typing import Any

type JSON = dict[str, Any]

METHODS = ("get", "post", "put", "patch", "delete")

_WORD_BOUNDARY = re.compile(r"(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])")


def json_media(media_type: str) -> bool:
    """application/json or a +json type, whatever its parameters (charset)."""
    media = media_type.split(";", 1)[0].strip().lower()
    return media == "application/json" or media.endswith("+json")


def scalar(value: object) -> str:
    """A value as the API writes it in a query or a default: a boolean is true or false."""
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def kebab(name: str) -> str:
    return _WORD_BOUNDARY.sub("-", name).lower()


def ref_name(node: JSON) -> str | None:
    ref = node.get("$ref")
    return ref.rsplit("/", 1)[-1] if isinstance(ref, str) else None


@dataclass(frozen=True)
class Parameter:
    name: str
    location: str
    required: bool
    description: str
    schema: JSON

    @property
    def option(self) -> str:
        return "--" + kebab(self.name)

    @property
    def dest(self) -> str:
        # Prefixed, so a parameter can never collide with the CLI's own options (--body, --all).
        return "p_" + self.name


@dataclass(frozen=True)
class Content:
    status: str
    description: str
    media_type: str | None
    schema: JSON | None

    @property
    def is_json(self) -> bool:
        return self.media_type is not None and json_media(self.media_type)


@dataclass(frozen=True)
class Operation:
    id: str
    method: str
    path: str
    summary: str
    description: str
    parameters: tuple[Parameter, ...]
    body: Content | None
    body_required: bool
    responses: tuple[Content, ...]

    @property
    def command(self) -> str:
        return kebab(self.id)

    def parameter(self, name: str) -> Parameter | None:
        return next((p for p in self.parameters if p.name == name and p.location == "query"), None)

    @property
    def paging(self) -> str | None:
        """How --all goes through the pages: by "offset", or "before" the last item; None without pages."""
        if self.parameter("limit") is None:
            return None
        return next((name for name in ("offset", "before") if self.parameter(name) is not None), None)


class Contract:
    def __init__(self, document: JSON) -> None:
        self.document = document
        self.operations = tuple(self._operations())

    def find(self, command: str) -> Operation | None:
        return next((op for op in self.operations if command in (op.command, op.id)), None)

    def resolve(self, node: JSON) -> JSON:
        for _ in range(32):
            ref = node.get("$ref")
            if not isinstance(ref, str):
                return node
            node = self._pointer(ref)
        raise ValueError(f"$ref loop at {node.get('$ref')}")

    def _pointer(self, ref: str) -> JSON:
        if not ref.startswith("#/"):
            raise ValueError(f"only local references are followed, not {ref}")
        node: Any = self.document
        for part in ref[2:].split("/"):
            node = node[part.replace("~1", "/").replace("~0", "~")]
        if not isinstance(node, dict):
            raise ValueError(f"{ref} is not an object")
        return node

    def _operations(self) -> list[Operation]:
        operations = []
        for path, item in self.document.get("paths", {}).items():
            shared = item.get("parameters", [])
            for method in METHODS:
                op = item.get(method)
                if op is None:
                    continue
                parameters: dict[tuple[str, str], Parameter] = {}
                for raw in [*shared, *op.get("parameters", [])]:
                    p = self.resolve(raw)
                    parameters[(p["name"], p["in"])] = Parameter(
                        name=p["name"],
                        location=p["in"],
                        required=bool(p.get("required")),
                        description=p.get("description", ""),
                        schema=p.get("schema", {}),
                    )
                body, body_required = None, False
                if "requestBody" in op:
                    request = self.resolve(op["requestBody"])
                    body_required = bool(request.get("required"))
                    for media_type, content in request.get("content", {}).items():
                        body = Content("", request.get("description", ""), media_type, content.get("schema"))
                        break
                responses = []
                for status, raw in op.get("responses", {}).items():
                    response = self.resolve(raw)
                    contents = response.get("content") or {None: {}}
                    for media_type, content in contents.items():
                        responses.append(
                            Content(str(status), response.get("description", ""), media_type, content.get("schema"))
                        )
                operations.append(
                    Operation(
                        id=op["operationId"],
                        method=method.upper(),
                        path=path,
                        summary=op.get("summary", ""),
                        description=op.get("description", ""),
                        parameters=tuple(parameters.values()),
                        body=body,
                        body_required=body_required,
                        responses=tuple(responses),
                    )
                )
        return operations
