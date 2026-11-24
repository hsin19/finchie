# proto

gRPC contracts, managed with [buf](https://buf.build/) (`buf.yaml` and `buf.gen.yaml` at the repository root).

| Package | Server | Client |
|---|---|---|
| `finchie.parser.v2` | `apps/parser` | `apps/backend` |

## Workflow

```sh
task proto          # format, lint, regenerate Go and Python code
task proto:check    # format, lint, and breaking-change check against main
```

Generated code is committed: Go in `apps/backend/internal/gen/`, Python in `apps/parser/src/finchie/`. Plugin versions in `buf.gen.yaml` must not be newer than the runtime libraries the services pin (`go.mod`, `pyproject.toml`); the generated code checks this when it loads.

## Conventions

- Amounts are `google.type.Decimal`, dates on statements are `google.type.Date` (ADR 0004).
- A breaking change means a new package version (`v3`), not an edit to `v2`.
