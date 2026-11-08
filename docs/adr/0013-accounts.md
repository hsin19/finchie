# ADR 0013: Accounts

## Decision

An account is a place where the owner keeps money or owes it: a deposit account, the bill of a bank's credit cards, and later a brokerage account, an exchange (InTheGreenYet), a points program. It exists in its own right. Sources tell Finchie what happens in it: statements are one, an exchange's API another, and more may come; none of them defines it. What an account is for, say that salary arrives in one and rent leaves from another, shows in how its money moves, not in how it is reported.

### Discovered, never set up

There is no account setup. A source that reports an account Finchie has not seen creates it, identified by institution, kind, and number; every later report of the same account finds it. The owner may give an account a name, but nothing needs one.

| Kind | One per | Number | Notes |
|---|---|---|---|
| `credit_card` | Institution: one bill | Empty | Its cards (last four digits) come from the lines, as today; one Richart bill lists several |
| `deposit` | Account number | As the bank prints it, usually masked (`12345**6789*`). The parser normalizes it so one account reads the same every month | One account can hold several currencies; its balance is per currency |

An account outlives every report of it: re-parsing or deleting a document never deletes one, so whatever is attached to an account stays.

### What sources report: lines and balances

- **Lines.** `transactions` gains `account_id` and `currency`; a line keeps its statement as where it came from. Deposit lines also carry `balance`, the running balance where the bank prints one, and `memo`. Their kind is `interest` or `fee` when the bank's own wording says so, else `other`. Telling card payments and transfers between the owner's accounts apart is for the transfer links to come, derived from the lines like ADR 0009's.
- **Balances.** `balances` holds what an account held in one currency at the end of a day, and which source said so: a statement's closing balance, its opening balance (as the end of the day before its period), an exchange's snapshot. A card bill's balance is −`total_due` on its closing date.

Balances are seen from the owner's side: money held is positive, money owed negative. Deposit lines follow that (money in is positive). Card lines keep the sign card bills print, a purchase positive, which the bill's arithmetic and ADR 0009 rely on; anything that adds card and deposit lines together, such as cash flow, flips the card lines.

### Statements are one source

- `statements` gains `kind` (`credit_card`, `deposit`). The card-only columns (`due_date`, `total_due`, the new-charge totals and label) become nullable, required by a check for `credit_card` only. A statement is identified by institution, kind, and closing date (was institution and closing date): a bank sends its card bill and its deposit statement on schedules of their own, possibly on the same day.
- A statement reports the lines and balances of each account it covers; a combined statement (綜合對帳單) covers several.
- Each parser reconciles its statement (ADR 0005), per account and currency: opening balance plus the lines must give the closing balance. Where lines print a running balance, each must also be the previous one plus its amount, which catches a dropped or misread line, not only a wrong total. An account a statement prints a balance for and nothing to add up does not make the statement `needs_review`.
- `ParseDocumentResponse` gains `DepositStatement deposit_statement = 5` next to `Statement statement = 3`, which stays the card bill; exactly one is set. A new field, because moving the existing one into a `oneof` is a change `buf breaking` rejects. A `DepositStatement` has the institution, the period's first and last day, and one section per account and currency: the number and name as printed, the currency, the opening balance (optional), the closing balance, its lines (date, description, memo, signed amount, running balance when printed, kind), and its reconciliation.

### Continuity: a gap is reported only when money moved unseen

Sources skip: a combined statement can be missing for a month without activity, and Shin Kong Securities sends a monthly statement only for months with trades. A missing report is therefore normal and never reported by itself. What is checked is that an account's balances join up: two consecutive balances of an account and currency join when the lines between them account for the difference. For statements that comes down to a statement's opening balance equalling the previous statement's closing balance, since a statement's own lines are its reconciliation. Other sources join the same way once they report movements (InTheGreenYet's ADR).

| Result | Meaning | Shown |
|---|---|---|
| Joins | Nothing happened that no source reported | Nothing |
| Does not join | A report is missing, or a balance was misread | The account is flagged with the two balances between which money moved unseen; `finchie.accounts.gaps` counts flagged accounts for alerting (ADR 0011) |
| Unknown | Nothing reports the movements between them (card bills until their parsers read the previous bill's amount; balance-only snapshots) | Nothing |

Like links (ADR 0009), continuity is derived, not kept by hand: reports arrive in any order, so an account's is worked out again whenever one of its balances is stored.

### Existing data

A migration creates one `credit_card` account per institution that has statements, sets every line's account and its statement's currency, and records each bill's −`total_due` as its account's balance on its closing date.

## Alternatives considered

- **Accounts as part of statements**, one row per account a statement covers. Simple while statements are the only source, but an exchange has no statements, and an account would be as many rows as reports of it.
- **One statement per account.** A combined statement is one document listing several accounts; splitting it would break one document, one statement (ADR 0005), which duplicates and re-parsing rely on.
- **A table of its own for deposit lines.** Card and deposit signs could never be mixed up, but search, the transactions page, and the links of ADR 0009 and of transfers would each have to span two tables.
- **Accounts set up by hand.** A setup step for what every source already reports.
- **Report every missing month.** Sources skip months on purpose; an alert that is usually wrong gets ignored.
- **Double-entry bookkeeping.** Every line would need its other side, a category, before it could be stored. Lines stay as reported and links are derived instead (ADR 0009).

## Consequences

- The web app gets an accounts page (latest balance per account and currency, flagged gaps), and search an account facet.
- The statements' uniqueness key changes; existing statements become `credit_card`.
- Net worth is the latest balance of every account and currency, whatever reported it; it needs exchange rates, which come with InTheGreenYet's ADR.
- What an account is for can later be read from its lines instead of being told: a salary that arrives every month, rent that leaves every month. One that stops is the kind of thing worth a notification (`AGENTS.md`, no work for the owner).
