# ADR 0021: Statements as account reports

## Decision

Every statement, whatever it is, reports accounts: their balances on a day and the lines between. A card bill reports the bank's card account and its points account (ADR 0013, ADR 0019), a deposit statement deposit accounts, a securities statement a brokerage account (ADR 0017). The parser's contract says so directly, as `finchie.parser.v2`, in place of one message per kind of statement (`Statement`, `DepositStatement`, `SecuritiesStatement` in v1).

```text
ParseDocumentResponse  parser, parser_version, statement, warnings
Statement              institution, period_start, closing_date, accounts[], checks[], card_bill
AccountReport          kind, number, name, balances[], lines[], expiries[]
Balance                unit, as_of, amount
Line                   date, unit, amount, kind, description, detail: card | deposit | trade
Check                  name, unit, computed, stated, balanced
CardBill               due_date, currency, total_due
```

- **One shape.** The backend already turned the three kinds into this shape before storing them; the contract now carries it, so there is one converter instead of three, and a new source (a loan statement, ADR 0022) is one more `AccountReport.kind`, not a new message.
- **Amounts as printed, signs as the account sees them.** A balance is what the statement prints for the account in the unit. A line's amount is as before: a card bill's charges positive and credits negative, as printed; a deposit line's money in positive; a trade's shares bought positive and sold negative; points added positive. The parser reads them off the statement; what each sign means to the owner is the backend's (`owner_flow`, ADR 0013).
- **What a statement says about a day is a balance.** A deposit statement's opening balance is a balance on the day before its period; a holding a balance on the holdings date; a points program's closing a balance on the closing date. The parser places each on its day; the backend no longer derives them.
- **A card bill keeps its own facts.** `CardBill` is what only a bill has: when it is due and what it says is owed. What the bill owes is also the card account's balance on the closing date (ADR 0022); the backend, not the parser, makes it so.
- **Every check a statement allows.** `checks` replaces the single `Reconciliation`: each is a figure the statement prints and the one its lines come to, with the statement's own label. A card bill's new charges, its payments subtotal, each points program, each deposit account's closing balance per currency, each of a securities statement's totals. A statement is balanced when every check is; one that is not makes the document `needs_review` (ADR 0005) and is stored as such, in `statement_checks`, instead of being folded into the first that failed or a sentence among the warnings.
- **Kinds.** One `LineKind` replaces the four enums of v1; one `AccountKind` names what each report is of.
- **Each account once.** An account is reported once per statement, all its units together: a deposit account in TWD and USD is one report with balances and lines in both.

The statement's kind (`credit_card`, `deposit`, `securities`), which its identity and the overlap of deposit exports depend on (ADR 0013), follows from what it reports: a bill when it has a `CardBill`, else securities when it reports a brokerage account, else deposit.

### Migration

v1 is retired in the same release: the parser and the backend are built and deployed together, and neither stores anything of the other's. `finchie/parser/v1` is deleted, and `buf breaking` no longer counts deleting a file as breaking (`FILE_NO_DELETE`): retiring a major version is how a contract changes incompatibly, and every other incompatible change within a version is still caught. Stored statements keep their rows; the card bills' single reconciliation becomes their first check (migration 00020).

## Alternatives considered

- **Keep the three messages, wrap them in a `oneof`.** The compiler would guarantee one, but every consumer would still branch three ways, and the fourth kind would be a fourth branch everywhere.
- **Owner-signed amounts in the contract.** One sign convention everywhere would drop `owner_flow`, but a card line would no longer read as the bill prints it, and every place that compares a line with its bill (reconciliation, refunds, payments) would turn signs. What the owner sees stays the backend's to work out.
- **Keep v1 beside v2 for a while.** Nothing calls the parser but this backend, which ships with it.
