# CI/CD workflows

## Layout

```
check.yml    push to main · workflow_call · workflow_dispatch
             → proto (format, lint, breaking) · generated code · parser · backend · web · mailbox · shellcheck
pr.yml       pull_request → check.yml · dependency review → auto-merge (Dependabot)
release.yml  v* tag       → check.yml → build each image on x64 + arm64 runners, smoke-check → push to GHCR
```

`check.yml` is the one definition of "is this tree good"; the other two call it, so PRs and releases meet the same bar. Add checks there, nowhere else. Every check runs through Task, so CI and `task check` on a laptop run the same commands. `main` has no deploy workflow, so `check.yml` also runs on every push to `main`, covering direct pushes and Dependabot merges.

| Job | What it guards |
|---|---|
| Proto contracts | `buf format`, `buf lint`, and `buf breaking`: against `main` on a PR, against the commit before the push on `main`. Not on release tags (their commit was compared when it reached `main`), nor while the baseline has no contracts. Locally `task proto:breaking` compares against `main`, or `AGAINST=<ref>` |
| Generated code is committed | Regenerates proto, sqlc, and oapi-codegen output and the Gmail filter; fails if anything differs from the commit |
| Parser | ruff, mypy (strict), then pytest on synthetic fixtures. Real statements are never in CI (ADR 0006) |
| Backend | build, golangci-lint, then tests. Integration tests start Postgres with testcontainers on the runner's Docker |
| Web | API types match `openapi.yaml`, dprint, ESLint, svelte-check, Vitest, then the production build |
| Mailbox | dprint, tsc, Vitest, then a bundle with `wrangler deploy --dry-run`. Deploys stay by hand (ADR 0010) |
| Shell scripts | shellcheck on `.github/scripts/` |

Within a job, each check is its own step gated on `!cancelled()`, so one run shows every failure instead of stopping at the first.

## Release

Push a tag: `git tag v2.0.0 && git push origin v2.0.0`. Images are `ghcr.io/<owner>/finchie-backend` and `ghcr.io/<owner>/finchie-parser`, tagged `2.0.0`, `sha-<short sha>`, and `2.0` when it is the highest final `v2.0.*` tag (so neither a pre-release such as `v2.0.0-rc.1` nor an older patch released late takes it).

- Each image is built from its app's `Dockerfile` on a native runner per architecture (`ubuntu-24.04`, `ubuntu-24.04-arm`). The backend's build context is the repository root, because it builds and embeds `apps/web/`.
- [`../scripts/smoke.sh`](../scripts/smoke.sh) then starts it the way production does, without secrets: the parser has to report gRPC health `SERVING`; the backend gets a throwaway Postgres 18, runs its migrations, has to answer `/healthz`, and has to serve the embedded web app. Run it locally the same way: `.github/scripts/smoke.sh backend finchie-backend:dev`.
- The image that passed is pushed as `<version>-<arch>` (e.g. `2.0.0-linux-arm64`); what is pushed is what ran.
- Only when all four builds pass does `publish` merge the two architectures into one index with `docker buildx imagetools create` and point every release tag at it. A failed release can leave `<version>-<arch>` tags behind, but never a release tag; deployments use release tags only.
- `publish` runs one release at a time per image, queued across tags, so two releases of one line cannot move its `major.minor` tag out of order.
- New GHCR packages are private. Make them public once in the package settings, or grant the deploying repository read access. The `org.opencontainers.image.source` label links each package to this repository.

## Dependency review

Until the **dependency graph** is on, `pr.yml`'s `dependency-review` job fails every PR with "Dependency review is not supported on this repository". Turning on Dependabot alerts (Settings → Advanced Security) turns it on. The job is also a prerequisite of `auto-merge`, so without the graph no Dependabot PR merges.

## Dependabot auto-merge

`pr.yml`'s `auto-merge` job hands a Dependabot PR whose `check` and `dependency-review` are green to the shared [`dependabot-automerge.yml`](https://github.com/hsin19/actions), which squash-merges it with `secrets.AUTOMERGE_TOKEN`. A dedicated token rather than `GITHUB_TOKEN`, because pushes made with `GITHUB_TOKEN` start no workflows, so `main` would skip `check.yml` after the merge.

1. Create a fine-grained personal access token for this repository only, with **Contents** and **Pull requests** read and write, and **no expiry**: an expired token keeps CI green while auto-merge silently stops.
2. Add it as a **Dependabot** secret named `AUTOMERGE_TOKEN` (Settings → Secrets and variables → Dependabot). Dependabot PRs read Dependabot secrets, not Actions secrets.

Groups whose names end in `-manual` are left for a human (see [`../dependabot.yml`](../dependabot.yml)):

| Group | Why it can't merge itself |
|---|---|
| `codegen-manual` (openapi-typescript) | Its output `apps/web/src/lib/api/schema.d.ts` is committed, so the web job's API-types check fails until `task web:generate` is run and committed |
| `images-manual` (Dockerfile base images), `compose-manual` | `check.yml` builds no images; the first real test of a new base image is a release |

Tool versions pinned in Taskfiles (`buf`, `golangci-lint`, `sqlc`, `oapi-codegen`) and the buf plugin versions in `buf.gen.yaml` are not tracked by Dependabot; bump them by hand. Keep buf plugin versions at or below the runtime libraries each service pins.
