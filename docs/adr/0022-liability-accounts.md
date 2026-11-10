# ADR 0022: Liability accounts

## Decision

An account is an asset or a liability. A card account and a loan are liabilities: what they hold is owed, and it counts against the owner. Both are accounts like any other, with balances on the days their sources report them and lines between.

| Kind | Side | Balance | Source |
|---|---|---|---|
| `deposit`, `securities`, `exchange`, `points` | asset | What it holds | Statements, snapshots, the owner (ADR 0013, 0017, 0018, 0019) |
| `credit_card` | liability | What a bill says is owed, on its closing date | Card bills (ADR 0021) |
| `loan` | liability | The principal outstanding | The owner (ADR 0019) for now; a loan statement once one is parsed |

- **Balances stay as printed; the side gives the sign.** A balance is stored as its source prints it: a bill's total due, a loan's principal, positive. Its worth, and the account's, is negative for a liability (`domain.AccountKind.Liability`). Net worth is every account's worth added up; there is no separate sum of what is owed.
- **A card account is the bank's.** One per bank, as before (ADR 0013), now listed among the owner's accounts, with its balance from each bill. ADR 0013 kept card accounts unlisted and without balances because a bill's amount due counted until the next bill even after it was paid, and the payment counted again from the deposit account it left. That is no card's problem alone: every source reports on its own day, and a transfer between two deposit accounts is counted twice or not at all until both have reported. Net worth is exact on the day every account has reported, and an estimate after it (ADR 0023).
- **Lines.** A card account's lines are its bills' lines, as they always were. Its continuity is not checked: a bill dates its lines by purchase, which can fall before the previous bill closed (ADR 0013); each bill checks itself instead (ADR 0021).
- **Payments.** A bill is still shown paid by a deposit outflow or a later bill (ADR 0016), and a loan's repayment from a deposit account is a transfer like any other once its statements are parsed.
- **By hand.** The owner may keep a loan (ADR 0019): its balance is the principal they say is outstanding, a snapshot like any other. A card account is never kept by hand: its bank's bills report it.

The API gives each account its `side`, and every kind's worth with its sign; `GET /api/net-worth` adds up assets and liabilities (ADR 0023).

## Alternatives considered

- **What is owed as a sum of unpaid bills, apart from accounts** (`domain.Owed`, briefly). It needed its own rule for which bills count and stopped at cards; a loan would have needed another.
- **A card account's balance as the last bill plus payments since.** Exact for cards between bills, but a rule only cards would have; every other account has the same gap between reports, and gets the same answer (ADR 0023).
- **Owner-signed balances** (a debt stored negative). The sign would be right in every sum, but a balance would no longer read as its source prints it, and the continuity check would need the sign of every line's kind as well.
