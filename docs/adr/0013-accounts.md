# ADR 0013: Accounts

## Decision

An account is where the owner's money sits at an institution: a deposit account, or the bill of a bank's credit cards. Every statement line belongs to one, and a statement reports the balance of each account it covers. Later kinds (securities, exchanges taken over from InTheGreenYet, points) come with their own ADRs and hang from the same table.

### Created by statements, never set up by hand

There is no account setup. The parser reports the accounts a statement covers as the statement prints them; the backend finds each by institution, kind, and number, and creates it the first time it appears.

| Kind | One per | Number | Notes |
|---|---|---|---|
| `credit_card` | Institution: one bill | Empty | Its cards (last four digits) come from the lines, as today; one Richart bill lists several |
| `deposit` | Account number | As the statement prints it, usually masked by the bank (`12345**6789*`). The parser normalizes it so one account reads the same every month | One account can hold several currencies; its balance is per currency |

Accounts outlive statements: re-parsing or deleting a document never deletes one, so whatever is later attached to an account stays.

### A statement covers accounts

- `statements` gains `kind` (`credit_card`, `deposit`). The card-only columns (`due_date`, `total_due`, the new-charge totals and label) become nullable, required by a check for `credit_card` only. A statement is identified by institution, kind, and closing date (was institution and closing date): a bank sends its card bill and its deposit statement on schedules of their own, possibly on the same day.
- `statement_accounts` has one row per account and currency a statement covers: the opening balance when printed, the closing balance, and their reconciliation. A card bill has one row, with −`total_due` as its closing balance; a combined statement (綜合對帳單) has one per account and currency.
- `transactions` gains `account_id` and `currency`. Deposit lines also carry `balance`, the running balance where the statement prints one, and `memo`. Their kind is `interest` or `fee` when the bank's own wording says so, else `other`; telling card payments and transfers between the owner's accounts apart is for the transfer links to come, derived from the lines like ADR 0009's.

Balances are seen from the owner's side: money held is positive, money owed negative. Deposit lines follow that (money in is positive). Card lines keep the sign card bills print, a purchase positive, which the bill's arithmetic and ADR 0009 rely on; anything that adds card and deposit lines together, such as cash flow, flips the card lines.

### Reconciliation

Each parser reconciles its own statements (ADR 0005), per account and currency: opening balance plus the lines must give the closing balance. Where lines print a running balance, each line's balance must also be the previous one plus its amount, which catches a dropped or misread line, not only a wrong total. A row that prints a balance and nothing to add up to it is stored as printed and does not make its statement `needs_review`.

### Continuity: gaps are reported only when money moved unseen

Banks skip months: a combined statement can be missing for a month without activity, and Shin Kong Securities sends a monthly statement only for months with trades. A missing month is therefore normal and never reported by itself. What is checked is that an account's statements join up: for each account and currency, a statement's opening balance must equal the closing balance of the account's previous statement.

| Result | Meaning | Shown |
|---|---|---|
| Joins | Nothing happened in the months skipped, if any | Nothing |
| Does not join | A statement is missing, or a balance was misread | The account is flagged with the two statements between which money moved unseen; `finchie.accounts.gaps` counts flagged accounts for alerting (ADR 0011) |
| Unknown | No opening balance printed (card bills until their parsers read the previous bill's amount) | Nothing |

Like links (ADR 0009), continuity is derived, not kept by hand: statements arrive in any order, so an account's is worked out again whenever one of its statements is stored.

### Parser contract

`ParseDocumentResponse` gains `DepositStatement deposit_statement = 5` next to `Statement statement = 3`, which stays the card bill; exactly one is set. A new field, because moving the existing one into a `oneof` is a change `buf breaking` rejects. A `DepositStatement` has the institution, the period's first and last day, and one section per account and currency: the number and name as printed, the currency, the opening balance (optional), the closing balance, its lines (date, description, memo, signed amount, running balance when printed, kind), and its reconciliation.

### Existing data

A migration creates one `credit_card` account per institution that has statements, and sets every line's account and its statement's currency.

## Alternatives considered

- **One statement per account.** A combined statement is one document listing several accounts; splitting it would break one document, one statement (ADR 0005), which duplicates and re-parsing rely on.
- **A table of its own for deposit lines.** Card and deposit signs could never be mixed up, but search, the transactions page, and the links of ADR 0009 and of transfers would each have to span two tables.
- **Accounts set up by hand.** A setup step for what every statement already prints.
- **Report every missing month.** Banks skip months on purpose; an alert that is usually wrong gets ignored.
- **Double-entry bookkeeping.** Every line would need its other side, a category, before it could be stored. Lines stay as printed and links are derived instead (ADR 0009).

## Consequences

- The web app gets an accounts page (latest balance per account and currency, flagged gaps), and search an account facet.
- The statements' uniqueness key changes; existing statements become `credit_card`.
- Net worth is the latest closing balance of every account and currency; it needs exchange rates, which come with InTheGreenYet's ADR.
