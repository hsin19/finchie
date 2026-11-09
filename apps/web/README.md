# web

The browser app (ADR 0007): a SvelteKit single-page app, embedded in the backend binary and served from the same origin as `/api`.

| Route | Page |
|---|---|
| `/` | Overview: this month's and last month's bills, what is due, the year so far, a 12-month trend, recent statements and documents |
| `/statements` | One month's bill (`?m=`, a closing month): what is due, spending, foreign transaction fees, rebates, by card and by kind, its statements. `?all` lists every statement, plus documents that produced none |
| `/statements/{id}` | One statement: metadata, reconciliation by kind, its lines in statement order, processing log, re-parse; `?line=` scrolls to a line |
| `/documents/{id}` | One document opened for reading (ADR 0012): an e-mail's headers, attachments, and body; the original to download even when the parser cannot open it |
| `/transactions` | Search across every account's lines, cards and deposit accounts alike: period, bank, card or account, kinds, overseas, text in the description or memo. Amounts are seen from the owner, money out negative (ADR 0013); totals are what went out and came in per unit. A transfer between the owner's lines or a card payment is one row, its outflow, with the line the money went to folded under it; it counts as neither but as money moved (ADR 0016), and `?hide=transfers` leaves it out |
| `/accounts` | The net of deposits, securities, exchanges, and card bills not seen paid, then every account (ADR 0013): its newest balance in each unit with its worth in NT$ (ADR 0015), a securities account's holdings at their newest close (ADR 0017), an exchange's coins (ADR 0018), a points account's programs and its next points to lapse (ADR 0019; red within 60 days), and whether its balances join up; accounts with gaps first, where money moved that no statement shows. They are ordered by worth; `?sort=bank` by bank. An account shows its worth first, to the whole NT$ and highlighted (one holding only NT$ highlights its balance instead), then its balances by worth, none at 0: the three largest of NT$1,000 or more, with how many others there are, the most any of them is worth, and how many have no rate. Accounts and balances under NT$1,000 either way, and balances without a rate, are left out of the list (never out of the totals) unless `?all`, which the account says it did; an account whose worth is not known always shows, and one with gaps is named above the list either way. "＋ 新增帳戶" opens an account kept by hand, of any kind but a card's (ADR 0019) |
| `/accounts/{id}` | One account: balances (a securities account's holdings with their close and worth), gaps, its lines with running balances and where linked ones' money went (ADR 0016), a trade's price and the money paid or received, a securities account's positions with their cost and gains where known, an exchange's worth (from its holdings, else the exchange's own valuation) and holdings with no lines, a points account's programs, its lines (each period's totals), and what its newest bill says will lapse by when, every balance it has had (an exchange's by day); the owner can name it and give its full number; an account kept by hand takes what it holds, a row a unit, and of points what lapses by when (ADR 0019); and each unit an account holds shows its newest price and where it came from, and takes the owner's in NT$ (ADR 0020) |
| `/upload` | Drop statement e-mails (`.eml`) and follow each through parsing; links the Gmail filter download |

Keys: `o` `s` `t` `a` `u` switch pages, `/` searches, `j` `k` `Enter` `n` `p` move through transactions, `f` opens a statement's PDF, `1`–`9` a document's attachments, `?` lists everything, `⌘K` opens the command palette.

A document opens in a new tab (`FileLink`, `openUrl` in `src/lib/documents.ts`): a PDF, or an e-mail a parser recognized (parsed, needs review, duplicate), as its statement PDF decrypted (`/api/documents/{id}/preview/pdf`); an e-mail that failed, was skipped, or is still parsing on `/documents/{id}`, to see what it is; anything else as the original download (`/api/documents/{id}/content`).

## Development

```sh
task web:install
task web:dev        # Vite on :5173; /api is proxied to the backend on :8080 (task backend:run)
task web:check      # what CI runs: dprint, ESLint, svelte-check, Vitest, build
task web:generate   # after apps/backend/api/openapi.yaml changes; commit src/lib/api/schema.d.ts
task backend:web    # build into the backend, so the backend serves it on :8080
```

Node 26 (`.node-version`); the pnpm version is pinned in `package.json`.

## Layout

| Path | |
|---|---|
| `src/routes/` | Pages. Data comes from universal `load` functions; filters live in the URL |
| `src/routes/layout.css` | Design tokens (`@theme`) and the few custom utilities (`panel`, `label`, `control`) |
| `src/lib/api/` | Generated types (`schema.d.ts`) and the `openapi-fetch` client |
| `src/lib/money.ts` | Exact decimal arithmetic and formatting for amounts (ADR 0004) |
| `src/lib/labels.ts` | Bank names, kind and status labels, tone classes |
| `src/lib/documents.ts` | Where a document opens |
| `src/lib/filters.ts` | The transactions page's filters, read from and written to the URL |
| `src/lib/flows.ts` | Amounts as the owner sees them, and which lines are transfers |
| `src/lib/keys.ts` | Which key presses are shortcuts: never while typing in a text field or composing with an IME, never while a dialog is open, never Enter or Space on a focused control |

## Pitfalls (SvelteKit 3)

- `#lib` is a package.json subpath import, so imports need the file extension: `#lib/money.js` (for `money.ts`), `#lib/components/Tag.svelte`.
- `+page.ts` / `+layout.ts` may only export SvelteKit's names (`load`, `ssr`, …); shared constants go in `src/lib/`. Type loads with `satisfies PageLoad`, not `: PageLoad`, or `data` loses its types.
- `svelte-check` reads proxy files that `svelte-kit sync` writes; run `pnpm typecheck`, which syncs first.
- `goto` options are `replace`, `reset: false` (keep scroll and focus), and `refreshAll`.
- `page.url` and `data` change only once a navigation's load is done. A control that changes the URL builds on the URL last asked for (`QueryRequests`), or a second change made while the first loads undoes it.
- SvelteKit sets the scroll position at the very end of a navigation, after the page's `$effect`s ran; scroll to something in `afterNavigate`.
- Internal links go through `resolve()` (`svelte/no-navigation-without-resolve`). Links that open a document use `FileLink`.
- A re-parse gives the statement a new id; the statement page follows it.
