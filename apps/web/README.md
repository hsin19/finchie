# web

The browser app (ADR 0007): a SvelteKit single-page app, embedded in the backend binary and served from the same origin as `/api`.

| Route | Page |
|---|---|
| `/` | Overview: this month's and last month's bills, what is due, the year so far, a 12-month trend, recent statements and documents |
| `/statements` | One month's bill (`?m=`, a closing month): what is due, spending, foreign transaction fees, rebates, by card and by kind, its statements; while a bank has not closed this month, no change against the month before. `?all` lists every statement |
| `/statements/{id}` | One statement: metadata, each check the bill prints against what its lines come to (ADR 0021; those that do not add up first, with what that likely means), its lines' sums by kind (money only), the points it reports (each program's lines, the balance after it, and, on the newest bill, what will lapse), its lines in statement order, processing log, re-parse; `?line=` scrolls to a line |
| `/documents` | Drop statement e-mails (`.eml`) and follow each through parsing; links the Gmail filter download. Then every document, newest first, loading more as the list scrolls (Back returns to where it was): what parsing it gave, by state (`?show=todo`, failed or not adding up; `pending`, `parsed`, `duplicate`, `skipped`), text in the filename (`?q=`). `/upload` redirects here |
| `/documents/{id}` | One document: what parsing it gave (what it produced and where, what to do about a failure, warnings) and a re-parse; opened for reading (ADR 0012): an e-mail's headers, attachments, and body; the original to download even when the parser cannot open it |
| `/transactions` | Search across every account's lines, cards and deposit accounts alike: period, bank, card or account, kinds, overseas, text in the description or memo. Amounts are seen from the owner, money out negative (ADR 0013); totals are what went out and came in per unit. A transfer between the owner's lines or a card payment is one row, its outflow, with the line the money went to folded under it; it counts as neither but as money moved (ADR 0016), and `?hide=transfers` leaves it out |
| `/accounts` | Net worth, assets less liabilities (ADR 0022), by kind, from `/api/net-worth`: exact as of the day every account had reported, an estimate after it naming the accounts behind (ADR 0023); a trend of it at each month end of the last year, estimates fainter; then every account, assets then liabilities (a bank's card account owes its newest bill, a loan its principal) (ADR 0013): its newest balance in each unit with its worth in NT$ (ADR 0015), a securities account's holdings at their newest close (ADR 0017), an exchange's coins (ADR 0018), a points account's programs and its next points to lapse (ADR 0019; red within 60 days), how many days ago its newest balance is, flagged when one is overdue (a statement past 50 days, the most a bank's took to come after closing plus a month; an exchange's reading past a day; never an account the owner keeps), without guessing why: a bank or a brokerage sends nothing for a month without a line, and whether its balances join up; accounts with gaps first, where money moved that no statement shows. They are ordered by worth; `?sort=bank` by bank. An account shows its worth first, to the whole NT$ and highlighted (one holding only NT$ highlights its balance instead), then its balances by worth, none at 0: the three largest of NT$1,000 or more, with how many others there are, the most any of them is worth, and how many have no rate. Accounts and balances under NT$1,000 either way, and balances without a rate, are left out of the list (never out of the totals) unless `?all`, which the account says it did; an account whose worth is not known always shows, and one with gaps is named above the list either way. "＋ 新增帳戶" opens an account kept by hand, of any kind but a card's, a loan included (ADR 0019, ADR 0022) |
| `/accounts/{id}` | One account: how current it is and whether it joins up, as on `/accounts`; balances (a securities account's holdings with their close and worth), gaps, its lines with running balances and where linked ones' money went (ADR 0016), a trade's price and the money paid or received, a securities account's positions with their cost and gains where known, an exchange's worth (from its holdings, else the exchange's own valuation) and holdings with no lines, a points account's programs, its lines (each period's totals), and what its newest bill says will lapse by when, every balance it has had (an exchange's by day); the owner can name it and give its full number; an account kept by hand takes what it holds, a row a unit, and of points what lapses by when (ADR 0019); and each unit an account holds shows its newest price and where it came from, and takes the owner's in NT$ (ADR 0020) |

Keys: `o` `s` `t` `a` `d` switch pages, `/` searches (the page's own search box, the documents' on `/documents`, else transactions), `j` `k` `Enter` `x` `Esc` `n` `p` move through transactions, `p` `n` `l` through statements, `f` opens a statement's PDF, `r` re-parses a statement or document, `1`–`9` a document's attachments, `?` lists everything, `⌘K` opens the command palette. Every key is listed once, in `src/lib/shortcuts.ts`: pages bind handlers only to keys listed there for them (`onKeys(scope, …)`), and the help dialog lists them from there.

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
| `src/lib/money.ts` | Exact decimal arithmetic (an average included: `divide`) and formatting for amounts (ADR 0004) |
| `src/lib/api/mutations.ts` | Every change the owner makes goes through `change()`: one way to report a failure, then the ledger reloads |
| `src/lib/institutions.ts` | The banks, brokers, and exchanges: names, codes, chart colors, how a kind of statement gets here where the owner has a part in it |
| `src/lib/kinds.ts`, `statuses.ts`, `tone.ts` | Account and line kinds, document status tags, and text colors by meaning |
| `src/lib/accounts.ts` | Account names and status, which balances a list shows, and what an account's page has by kind (`accountPage`); `/accounts`'s URL |
| `src/lib/statements.ts` | A card bill's tag, whether it adds up, its summary line, and the points it reports |
| `src/lib/bills.ts` | A month's bill, and what is still due |
| `src/lib/transactions.ts` | A transaction as rows and its detail show it |
| `src/lib/documents.ts` | Where a document opens, the document list's filters and URL, what parsing a document gave, and an upload's row |
| `src/lib/filters.ts` | The transactions page's filters, read from the URL and written to it (`transactionsHref`), and its account picker |
| `src/lib/urls.ts` | The statements pages' URLs (`?m=`, `?all`, `?line=`), read and written |
| `src/lib/nav.ts` | Where the header's links go from the page on screen, and which entity a page shows |
| `src/lib/search.svelte.ts` | A search box whose text is in the URL (`SearchBox.svelte`), and the page's box for `/` |
| `src/lib/upload.svelte.ts` | The uploads of a visit to `/documents` (`api/upload.ts` sends them) |
| `src/lib/poll.ts` | The loop that looks at pending documents while the parser works |
| `src/lib/shortcuts.ts` | Every keyboard shortcut, for the handlers and the help dialog |
| `src/lib/flows.ts` | Amounts as the owner sees them, and which lines are transfers |
| `src/lib/keys.ts` | Which key presses are shortcuts: never while typing in a text field or composing with an IME, never while a dialog is open, never Enter or Space on a focused control |

## Pitfalls (SvelteKit 3)

- `#lib` is a package.json subpath import, so imports need the file extension: `#lib/money.js` (for `money.ts`), `#lib/components/Tag.svelte`.
- `+page.ts` / `+layout.ts` may only export SvelteKit's names (`load`, `ssr`, …); shared constants go in `src/lib/`. Type loads with `satisfies PageLoad`, not `: PageLoad`, or `data` loses its types.
- `svelte-check` reads proxy files that `svelte-kit sync` writes; run `pnpm typecheck`, which syncs first.
- `goto` options are `replace`, `reset: false` (keep scroll and focus), and `refreshAll`.
- `page.url` and `data` change only once a navigation's load is done. A control that changes the URL builds on the URL last asked for (`QueryRequests`), or a second change made while the first loads undoes it.
- SvelteKit sets the scroll position at the very end of a navigation, after the page's `$effect`s ran; scroll to something in `afterNavigate`.
- Internal links go through `resolve()` (`svelte/no-navigation-without-resolve`). A link with a query comes from the builder next to what reads that query (`transactionsHref`, `statementsHref`, `statementHref`, `accountsHref`, `documentsQueryHref`), typed `ResolvedPathname` so the lint rule takes it. Links that open a document use `FileLink`.
- Reloading: every load that reads the ledger calls `depends(LEDGER)` itself. A change (`change()`) and a parse that finished invalidate it, nothing else; the document list's load alone does not depend on it, since reloading its first page would drop the pages loaded since. While anything is pending, the layout's poll fetches only the pending documents and invalidates the ledger once they change; a failed look is tried again later.
- The layout's `statements` and `documents` are the newest 500, a window: fine for what is recent, never a complete join. `pending` is every pending document. A page that shows one document or statement loads it itself; `/documents` asks again by id (`?id=`) for a listed document that finished outside the window.
- A search box's text and the URL's `?q=` meet in `Search` (`search.svelte.ts`): use `SearchBox`, not a hand-made debounce.
- SvelteKit keeps a page's component when only a route parameter changes (`/accounts/7` → `/accounts/9`), so its `$state` would carry over to the other entity. The root layout remounts the page when its parameters change (`entityKey` in `src/lib/nav.ts`); a query change (a filter) keeps it.
- A re-parse keeps the statement's id but gives its lines new ones; the statement page reloads with the ledger once the parse is done.
