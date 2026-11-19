"""What `finchie --help` and `finchie <command> --help` print, all of it from the contract.

A command's help is everything an agent needs to call it and read what comes back: its options, and
the fields of its answer with what the contract says of each.
"""

from finchie_cli.contract import JSON, Contract, Operation, Parameter, kebab, ref_name, scalar


def overview(contract: Contract, url: str) -> str:
    width = max(len(op.command) for op in contract.operations)
    commands = "\n".join(f"  {op.command:<{width}}  {op.method:<6}  {op.summary}" for op in contract.operations)
    return f"""\
finchie: the Finchie ledger at {url}, for agents and scripts.

Usage:
  finchie <command> [options]
  finchie <command> --help    its options, and the fields of what it returns

Output is JSON on stdout: indented on a terminal, compact otherwise. A file (a statement's PDF) comes
as its bytes. --all, where a command offers it, goes through every page and prints one item per line.
An error from the API goes to stderr with exit status 1; a wrong command or option exits with 2.
Amounts are decimal strings (Decimal) with their unit beside them: add them as decimals, not floats.
Through Cloudflare Access with the agents' service token, only GET commands are allowed.

Commands:
{commands}
"""


def operation(contract: Contract, op: Operation) -> str:
    positional = " ".join(f"<{kebab(p.name)}>" for p in op.parameters if p.location == "path")
    lines = [
        f"finchie {op.command}{' ' + positional if positional else ''} [options]",
        f"{op.method} {op.path}",
        "",
        op.summary,
    ]
    if op.description.strip():
        lines += ["", op.description.strip()]

    options = [(f"<{kebab(p.name)}>", _parameter(contract, p)) for p in op.parameters if p.location == "path"]
    options += [(_option(contract, p), _parameter(contract, p)) for p in op.parameters if p.location == "query"]
    if op.body is not None:
        if op.body.is_json:
            needed = " (required)" if op.body_required else ""
            options.append(
                ("--body JSON", f"The request's body{needed}: JSON, @file to read it from a file, - for stdin")
            )
        else:
            options.append(("", f"Takes a {op.body.media_type} body, which this CLI does not send"))
    if op.paging is not None:
        how = "every page, the offset counted up" if op.paging == "offset" else "every page, each before the last"
        options.append(("--all", f"Fetch {how}, and print the items one per line (NDJSON) instead of the pages"))
    if options:
        width = min(max(len(name) for name, _ in options), 28)
        lines += ["", "Options:"] + [f"  {name:<{width}}  {text}".rstrip() for name, text in options]

    fields = _Fields(contract)
    if op.body is not None and op.body.is_json and op.body.schema is not None:
        lines += ["", "Body:", *fields.render(op.body.schema, 1)]
    for content in op.responses:
        if not content.status.startswith("2"):
            continue
        heading = f"Returns {content.status}"
        if content.media_type is None:
            lines += ["", f"{heading}: nothing. {_line(content.description)}".rstrip()]
        elif content.is_json and content.schema is not None:
            lines += ["", f"{heading} ({content.media_type}):", *fields.render(content.schema, 1)]
        else:
            lines += ["", f"{heading} ({content.media_type}): the bytes, on stdout. {_line(content.description)}"]
    errors = [f"{c.status} {_line(c.description)}" for c in op.responses if not c.status.startswith("2")]
    if errors:
        lines += ["", "Errors:", *(f"  {e}" for e in errors)]
    return "\n".join(lines) + "\n"


def _option(contract: Contract, p: Parameter) -> str:
    schema = contract.resolve(p.schema)
    if schema.get("type") == "boolean":
        return f"{p.option}, --no-{p.option[2:]}"
    return f"{p.option} {_metavar(contract, schema)}"


def _metavar(contract: Contract, schema: JSON) -> str:
    if schema.get("type") == "array":
        return _metavar(contract, contract.resolve(schema.get("items", {})))
    if "enum" in schema:
        return "VALUE"
    if schema.get("format") == "date":
        return "DATE"
    if schema.get("type") == "integer":
        return "N"
    return "TEXT"


def _parameter(contract: Contract, p: Parameter) -> str:
    schema = contract.resolve(p.schema)
    notes = [_line(p.description)] if p.description else []
    items = contract.resolve(schema.get("items", {})) if schema.get("type") == "array" else schema
    if "enum" in items:
        notes.append("One of: " + ", ".join(str(v) for v in items["enum"]))
    if schema.get("type") == "array":
        notes.append("Repeat it for more than one")
    if p.required and p.location != "path":
        notes.append("Required")
    if "default" in schema:
        notes.append(f"Default {scalar(schema['default'])}")
    if "maximum" in schema:
        notes.append(f"At most {schema['maximum']}")
    return ". ".join(n.rstrip(".") for n in notes if n)


class _Fields:
    """A schema as lines of fields, each named schema spelled out the first time it appears only."""

    def __init__(self, contract: Contract) -> None:
        self._contract = contract
        self._seen: set[str] = set()

    def render(self, schema: JSON, depth: int) -> list[str]:
        label, expand, about = self._label(schema)
        lines = [f"{'  ' * depth}{label}{_about(_line(schema.get('description', '')) or about)}"]
        if expand is not None:
            lines += self._properties(expand, depth + 1)
        return lines

    def _properties(self, schema: JSON, depth: int) -> list[str]:
        required = set(schema.get("required", []))
        lines = []
        for name, prop in schema.get("properties", {}).items():
            label, expand, about = self._label(prop)
            optional = "" if name in required else "?"
            text = _line(prop.get("description", "")) or about
            lines.append(f"{'  ' * depth}{name}{optional}: {label}{_about(text)}")
            if expand is not None:
                lines += self._properties(expand, depth + 1)
        return lines

    def _label(self, node: JSON) -> tuple[str, JSON | None, str]:
        """The type in words, the object whose fields follow, and what the named type says of itself."""
        all_of = node.get("allOf")
        if isinstance(all_of, list) and len(all_of) == 1:
            # allOf of one is how the contract gives a $ref a description or nullable of its own.
            node = {**all_of[0], **{k: v for k, v in node.items() if k != "allOf"}}
        name = ref_name(node)
        schema = self._contract.resolve(node)
        nullable = ", nullable" if node.get("nullable") or schema.get("nullable") else ""
        first = name is not None and name not in self._seen
        if name is not None:
            self._seen.add(name)
        about = _line(schema.get("description", "")) if first else ""

        kind = schema.get("type")
        if kind == "array":
            label, expand, inner = self._label(schema.get("items", {}))
            return f"array of {label}{nullable}", expand, about or inner
        if kind == "object" or "properties" in schema:
            if "properties" not in schema:
                extra = schema.get("additionalProperties")
                if isinstance(extra, dict):
                    label, expand, inner = self._label(extra)
                    return f"map of text to {label}{nullable}", expand, about or inner
                return f"{name or 'object'}{nullable}", None, about
            if name is None:
                return f"object{nullable}", schema, about
            if not first:
                return f"{name} (as above){nullable}", None, ""
            return f"{name}{nullable}", schema, about
        if "allOf" in schema or "oneOf" in schema or "anyOf" in schema:
            return f"{name or 'any'}{nullable}", None, about

        scalar = str(kind or "any") + (f" ({schema['format']})" if "format" in schema else "")
        values = " | ".join(str(v) for v in schema.get("enum", []))
        if name is None:
            return f"{scalar}{': ' + values if values else ''}{nullable}", None, about
        if not first:
            return f"{name}{nullable}", None, ""
        return f"{name} ({values or scalar}){nullable}", None, about


def _line(text: str) -> str:
    return " ".join(text.split())


def _about(text: str) -> str:
    return f" — {text}" if text else ""
