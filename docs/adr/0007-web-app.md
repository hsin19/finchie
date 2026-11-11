# ADR 0007: Web app

## Decision

The web app is a SvelteKit single-page app in `apps/web/`, embedded in the backend binary and served from the same origin as the API.

| Area | Choice | Why |
|---|---|---|
| Framework | SvelteKit 3 (Svelte 5 runes) | The owner already writes Svelte 5 (show-me-way uses plain Svelte and Vite); SvelteKit's router and load functions are the new part to practise |
| Rendering | Single-page app: `adapter-static` with a `200.html` fallback, `ssr = false` | One user behind Cloudflare Access gains nothing from server rendering, and it would add a Node process to deploy |
| Serving | The backend embeds `apps/web/build` with `go:embed` and answers every non-API GET with `200.html` | One image, one origin: no CORS, one Access application, the UI and API always ship the same version |
| API client | `openapi-typescript` types from `apps/backend/api/openapi.yaml`, called through `openapi-fetch` | The same contract-first path as the Go handlers; the generated file is committed and CI fails when it drifts |
| Data loading | Universal `load` functions; every filter lives in the URL | A filtered view can be bookmarked, and Back undoes a filter |
| Live status | The layout polls while any document is `pending` (backing off from 1.5 s to 15 s, and pausing while the tab is hidden) | Parsing takes seconds; polling is simpler than a push channel |
| Money | Decimal strings added as scaled `BigInt`s (`apps/web/src/lib/money.ts`) | ADR 0004 holds in the browser too; floating point only sizes bars |
| Styling | Tailwind v4; design tokens in `@theme` in `apps/web/src/routes/layout.css` | |
| Fonts | JetBrains Mono and Noto Sans TC, self-hosted through Fontsource | No requests to third-party font hosts; Noto's unicode-range subsets load only the characters a page uses |
| Tooling | pnpm, Vite, Vitest, ESLint with `eslint-plugin-svelte`, svelte-check, dprint | The same set as show-me-way |

### Design: "terminal"

Dark only, monospace everywhere, one amber accent. Pages read like a shell: the header is the command that shows the page (`finchie transactions --period 2026-10`, flags spelled out in full), statuses are a coloured dot and a short word (`● ok`), section labels are upper-case codes with a Chinese gloss (`SPEND · 本月消費`), and bars are dashed meters.

| Token | Value | Use |
|---|---|---|
| `ground` | `#0a0a0b` | Page background |
| `panel` / `raised` | `#111114` / `#1b1b20` | Boxes / selected row |
| `line` / `line-strong` / `rule` | `#26262b` / `#3a3a41` / `#1c1c20` | Borders, control outlines, row separators |
| `ink` / `dim` / `mute` | `#e4e4e7` / `#a1a1aa` / `#8b8b94` | Text, secondary text, labels |
| `accent` | `#ffb020` | Keys, links, focus, the current page |
| `pos` / `neg` | `#3ddc97` / `#ff6b6b` | Refunds and rebates / fees and problems |

| Status | Meaning |
|---|---|
| `● ok` | Parsed and reconciled |
| `● run` / `● up` | Parsing / uploading |
| `● chk` | Parsed, but the lines don't add up (`needs_review`) |
| `● dup` / `● same` | Another document holds this statement / these exact bytes were uploaded before |
| `● err` / `● skip` | Parsing failed / no parser handles this kind of file |

Square brackets mean one thing: a key you can press (`[a] 全部帳單`), the key in amber and its label in plain text. Statuses, toggles, and filters don't use them; a selected filter shows by its amber outline.

Single-key shortcuts drive every page (`o` `s` `t` `u` to switch pages, `/` to search, `j` `k` `Enter` in lists, `?` for help, `⌘K` for a command palette). (Now `o` `s` `t` `a` `d` switch pages, and `/` searches the page's own list or, without one, the transactions. One table, `apps/web/src/lib/shortcuts.ts`, drives every handler and the help dialog, so the keys are listed there and nowhere else.) Phones get a bottom tab bar and lists grouped by day instead of tables.

## Alternatives considered

- **React (Next.js, React Router, TanStack).** Well known to the owner, so less to practise; Svelte needs less code for the same page.
- **Astro.** Built for content sites; this app is filters, polling, and shortcuts, which would all be client-side islands.
- **Server-rendered SvelteKit (`adapter-node`).** A second runtime service for one user.
- **Connect or gRPC-Web from the browser.** A second API surface next to the REST one that already exists.
- **Other designs.** A frosted-glass look like InTheGreenYet needs a background image and blur on every panel; the bento and ledger mock-ups looked too much like show-me-way. The terminal look is distinct and suits a dense ledger.
