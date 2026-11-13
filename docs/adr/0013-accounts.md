# ADR 0013: Accounts

## Decision

An account is anything that holds a value for the owner: a deposit account in NT$ or in foreign currencies, and later a brokerage account, an exchange (InTheGreenYet), a card's points, an airline's miles, a hotel's points. It exists in its own right. Sources tell Finchie what happens in it: statements are one, an exchange's API another, and more may come; none of them defines it. What an account is for, say that salary arrives in one and rent leaves from another, shows in how its money moves, not in how it is reported.

### Discovered, never set up

There is no account setup. A source that reports an account Finchie has not seen creates it, identified by institution, kind, and number; every later report of the same account finds it. (ADR 0019 adds accounts the owner keeps by hand, as an optional supplement; those are found by id.) The owner may give an account a name, but nothing needs one.

| Kind | One per | Number | Notes |
|---|---|---|---|
| `credit_card` | Institution: one bill | Empty | Not an account the owner has: a container for the bank's card lines, which need one to belong to. It holds no balance and is not listed among the accounts. (Since ADR 0022 it is a liability, listed, its balance on each closing date what that bill says is owed.) Its cards (last four digits) come from the lines, as today; one Richart bill lists several |
| `deposit` | Account number | As the bank prints it, usually masked (`12345**6789*`). The parser normalizes it so one account reads the same every month | One account can hold several currencies; its balance is per currency |

More kinds (securities, exchanges, points and miles programs) are added with the first source that reports one (`securities` came with ADR 0017, `exchange` with ADR 0018, `points` with ADR 0019).

An account outlives every report of it: re-parsing or deleting a document never deletes one, so whatever is attached to an account stays.

### What sources report: lines and balances

- **Lines.** `transactions` gains `account_id`, `unit`, and `status`; a line keeps its statement as where it came from. Deposit lines also carry `balance`, the running balance where the bank prints one, and `memo`. Their kind is `interest` or `fee` when the bank's own wording says so, else `other`. Telling card payments and transfers between the owner's accounts apart is for the transfer links to come, derived from the lines like ADR 0009's.
- **Balances.** `balances` holds what an account held in one unit at the end of a day, and which source said so: a statement's closing balance, its opening balance (as the end of the day before its period), an exchange's snapshot.

Balances are seen from the owner's side: money held is positive, money owed negative. Deposit lines follow that (money in is positive). Card lines keep the sign card bills print, a purchase positive, which the bill's arithmetic and ADR 0009 rely on; anything that adds card and deposit lines together, such as cash flow, flips the card lines.

### Units: money, and values that are not

Every amount has a unit (ADR 0004 says a currency): the ISO 4217 code for money, or a program's own code for points and miles, one per program, since one bank's points and an airline's miles never add up. Columns that held a currency as `CHAR(3)` take a unit as `TEXT` (ADR 0020: a row refers to a unit in `units`, its kind and symbol). What a point is worth in NT$, for net worth, comes with the first points source (came with ADR 0020's prices).

### Values that expire

Points and miles lapse, by rules of three sorts: rolling (the whole balance stays valid while the account has activity within a period), fixed (each amount earned lapses a set time after it was earned, or on a set date), or never. An account carries its rule, set by the source that knows the program, never by the owner. Only `none` exists for now, and every account has it; rolling and fixed rules, with what they need (the period, the date), come with the first points source. (Corrected by ADR 0019: card bills print what lapses by when, which is kept as reported, so no rule is needed and every account keeps `none`.)

A line's `status` is `active`, or `expired` when a value it brought lapsed, so expiry can be shown without changing or deleting the line as reported. Every line is `active` for now. A rule that spends the oldest points first may have to split a line partly spent before it lapsed; that too is for the first points source. (A card bill prints a period's totals, which ADR 0019 keeps as lines, one per column; none of them is a single earning to split.)

### Statements are one source

- `statements` gains `kind` (`credit_card`, `deposit`). The card-only columns (`due_date`, `total_due`, the new-charge totals and label) become nullable, required by a check for `credit_card` only. A statement is identified by institution, kind, and closing date (was institution and closing date): a bank sends its card bill and its deposit statement on schedules of their own, possibly on the same day. (And `period_start`, so two on-demand e-passbook exports that end on the same day while starting on different days are not taken for duplicates; when two deposit statements of an account in a currency share days, only the lines and balances of the one closing later stand over them, and of two closing the same day the later document's, whatever order they are stored in. Statements that only adjoin, as monthly ones do, share no day. And since migration 00014, the accounts it reports, `account_numbers`: two brokerage accounts' monthly statements, or two e-passbook exports of different accounts on the same days, are two statements, never one the other's duplicate.)
- A statement reports the lines and balances of each account it covers; a combined statement (綜合對帳單) covers several.
- Each parser reconciles its statement (ADR 0005), per account and currency: opening balance plus the lines must give the closing balance. Where lines print a running balance, each must also be the previous one plus its amount, which catches a dropped or misread line, not only a wrong total. An account a statement prints a balance for and nothing to add up does not make the statement `needs_review`.
- `ParseDocumentResponse` gains `DepositStatement deposit_statement = 5` next to `Statement statement = 3`, which stays the card bill; exactly one is set. A new field, because moving the existing one into a `oneof` is a change `buf breaking` rejects. A `DepositStatement` has the institution, the period's first and last day, and one section per account and currency: the number and name as printed, the currency, the opening balance (optional), the closing balance, its lines (date, description, memo, signed amount, running balance when printed, kind), and its reconciliation.

### Continuity: a gap is reported only when money moved unseen

Sources skip: a combined statement can be missing for a month without activity, and Shin Kong Securities sends a monthly statement only for months with trades. A missing report is therefore normal and never reported by itself. What is checked is that an account's balances join up: two consecutive balances of an account and unit join when the lines between them account for the difference. For statements that comes down to a statement's opening balance equalling the previous statement's closing balance, since a statement's own lines are its reconciliation. Other sources join the same way once they report movements (InTheGreenYet's ADR).

| Result | Meaning | Shown |
|---|---|---|
| Joins | Nothing happened that no source reported | Nothing |
| Does not join | A report is missing, or a balance was misread | The account is flagged with the two balances between which money moved unseen; `finchie.accounts.gaps` counts flagged accounts for alerting (ADR 0011) (unless the owner filled the gap once its reports were lost for good: a line of kind `fill`, migration 00021) |
| Unknown | Nothing reports the movements between them (balance-only snapshots) | Nothing (also when a deposit account's balance moves in a unit no statement has listed a line of: CTBC lists only the balance of a passbook savings account, 活儲有摺, so whatever moves it, interest included, no statement shows. A balance that never moved still joins; a securities statement lists every trade, so a security never traded is still checked.) |

Like links (ADR 0009), continuity is derived, not kept by hand: reports arrive in any order, so an account's is worked out again whenever one of its balances is stored. (It is worked out on every read instead, by `domain.Assess`, so it is never stale.)

### Existing data

A migration creates one `credit_card` account per institution that has statements, and sets every line's account, unit (its statement's currency), and status (`active`).

## Alternatives considered

- **A card bill as an account, its amount due as the balance** (what this ADR first said). A bill reports what is owed on one day, not a balance that carries on: once paid, it still counted until the next bill, the payment counted again from the deposit account it left, and nothing could check either.
- **Accounts as part of statements**, one row per account a statement covers. Simple while statements are the only source, but an exchange has no statements, and an account would be as many rows as reports of it.
- **One statement per account.** A combined statement is one document listing several accounts; splitting it would break one document, one statement (ADR 0005), which duplicates and re-parsing rely on.
- **A table of its own for deposit lines.** Card and deposit signs could never be mixed up, but search, the transactions page, and the links of ADR 0009 and of transfers would each have to span two tables.
- **Accounts set up by hand.** A setup step for what every source already reports. (ADR 0019 allows them as a supplement for what no source reports, never as a step any feature needs.)
- **Report every missing month.** Sources skip months on purpose; an alert that is usually wrong gets ignored.
- **Double-entry bookkeeping.** Every line would need its other side, a category, before it could be stored. Lines stay as reported and links are derived instead (ADR 0009).

## Consequences

- The web app gets an accounts page (latest balance per account and unit, flagged gaps), and search an account facet.
- The statements' uniqueness key changes; existing statements become `credit_card`.
- ADR 0004 gets a note: a points program's unit counts as an amount's currency.
- What a card bill says is owed stays on the bill (`total_due`, `due_date`); whether it was paid comes with the transfer links, and net worth takes off only the bills not yet paid.
- Net worth is the latest balance of every account and unit, whatever reported it; it needs exchange rates, which come with InTheGreenYet's ADR (came first, with ADR 0015), and for points their worth.
- What an account is for can later be read from its lines instead of being told: a salary that arrives every month, rent that leaves every month. One that stops is the kind of thing worth a notification (`AGENTS.md`, no work for the owner).
