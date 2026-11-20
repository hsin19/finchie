# ADR 0003: Tech stack

## Decision

Finchie is a side project. Trying interesting technology and practising it is an explicit goal, as long as each piece solves a real need here. Libraries should be current and well-liked but not niche: widely used, actively maintained.

| Area | Choice | Why |
|---|---|---|
| Backend language | Go | Practice; small static binary; simple concurrency for jobs |
| HTTP API | Echo v5 + oapi-codegen (strict server) | Contract-first like the proto: `api/openapi.yaml` generates typed handlers, and later a TypeScript client. Revisit when the web client is chosen (REST client vs. Connect) (ADR 0007 kept REST: openapi-typescript and openapi-fetch) |
| Database access | pgx v5 + sqlc | Type-safe Go generated from plain SQL |
| Migrations | goose, embedded in the binary, applied at startup | Plain SQL files, no separate migration step to forget |
| Background jobs | River | Durable jobs, retries, and periodic jobs stored in the same Postgres; no Redis or message broker |
| RPC | gRPC (`grpc-go`, `grpcio`) with buf | One contract, code for both languages, breaking-change checks |
| Parser language | Python 3.13 (3.14 since November 2026; the image and the tests' `.python-version` move together, by hand) | The best PDF and OCR tooling |
| PDF | pikepdf (decrypt), pdfplumber (text and positions), pypdfium2 (render pages for OCR) (since November 2026 pypdfium2 decrypts too, and pikepdf only builds the tests' encrypted PDFs: it and lxml, which only it used, were about 30 MB of the image) | All pip-installable |
| OCR | RapidOCR (ONNX runtime) | HSBC prints transaction descriptions as images. Reads Chinese and Latin text, needs no system packages; the image loads its models at build time |
| Integration tests | testcontainers-go with the homelab's Postgres version | Tests run against real Postgres, not mocks |
| Parser settings | pydantic-settings | Typed settings from environment variables |
| Python tooling | uv, ruff, pytest | Carried over from V1 |
| Web | TypeScript; framework chosen when work starts (SvelteKit, ADR 0007) | |
| Deployment | Images on GHCR, homelab Docker Compose, Cloudflare Tunnel and Access | Same path as the homelab's other services |
| Developer tools | Pinned through Go `tool` directives or `go run module@version` (all through `go run` now: as `tool` directives, sqlc's and oapi-codegen's dependencies counted as the backend's and their advisories failed dependency review) | Nothing to install globally besides Go, uv, Task, and Docker |

## Alternatives considered

- **.NET for the backend.** Well known to the owner, with code to borrow from earlier projects. Rejected because practising something new is the point.
- **Go only, parsing through command-line tools.** Go's PDF libraries are weak (`ledongthuc/pdf`), paid (`unipdf`), or AGPL through cgo (`go-fitz`). Shelling out to `pdftotext` was possible but unverified on these statements, while pdfplumber already parses all four banks.
- **Python only.** Fewer languages, but no Go practice.
- **Tesseract for OCR.** Needs system packages on every machine; RapidOCR installs with pip.
