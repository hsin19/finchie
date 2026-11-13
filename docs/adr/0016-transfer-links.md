# ADR 0016: Transfer links

## Decision

Money that moves between two of the owner's lines is linked: a deposit account paying a card bill, or one account paying into another. A line points at its counterpart (`transactions.counterpart_id`, both ways), and a card bill knows when it was paid (`statements.paid_on`, `paid_by`). Like ADR 0009's parents, links are worked out from the lines, never kept by hand: every time a statement is stored, and once at start, they are cleared and worked out again from all the lines, under the same advisory lock.

| Link | When |
|---|---|
| Card payment | A deposit account's outflow and a bill's payment line (`payment`) of the same amount, at most 3 days apart. Either bank's: a card is paid from whichever account |
| Own transfer | An outflow of one deposit account and an inflow of another, of the same amount, at most 3 days apart (now 10: one bank posts a transfer made over the Lunar New Year on its next working day, and the fitting counterparty is the proof), and at least one of them prints a counterparty that fits the other account. A pair that prints none is not linked: two equal amounts prove nothing by themselves |
| Exchange | An outflow in one unit and an inflow in another, at the same bank, on the same day, with the same memo: the bank prints the exchange's reference on both sides (Next Bank: `新臺幣換美元;@31.5000;FT…`). The amounts differ by the rate, so they prove nothing; a bank that prints no common reference leaves its exchanges unlinked |
| To or from a crypto exchange | (Added with ADR 0018's deposits and withdrawals.) A deposit account's outflow and an exchange's deposit, or an exchange's withdrawal and a deposit account's inflow, of the same amount and unit, at most 3 days apart. The exchange's own record of the money arriving is the proof a counterparty gives between two banks |
| Bill paid | A card bill is paid on the day an outflow of its total due leaves a deposit account, or a later bill of its bank shows a payment of it, between its closing and 5 days past its due date. A bill that owes nothing is paid on its closing date |

Each line is in one link at most; nearer days pair first, then card payments before own transfers and exchanges. Amounts that must be the same are in the same unit, a bill's total due included.

**A counterparty fits an account** when its bank code is the account's bank's (none printed means the line's own bank; a bank that names the other side's bank instead has its parser turn the name into the code) and, right-aligned with leading zeros dropped, no digit both print differs and at least 4 do agree. Banks print counterparties masked as well (`812 0087654321***234`); an account's full number, where the owner gave one, fits with every digit.

### What links are for

- A transfer between the owner's accounts is no income and no spending; a card payment is not spending either, the purchases on the bill were. Still, money moved: a list of lines shows each link once, as its outflow, with the line the money went to (another account's inflow, the bill's payment line) folded under it like ADR 0009's children, and totals show what moved apart from what was spent and received. A list without the outflow, of another account or another month, shows the other line itself. Unlike a parent, a link is no `parent_id`: the two lines are the same money on two accounts, not a purchase and what adjusts it, and adding them up as a parent's amount would count it twice.
- What net worth takes off for cards is the newest bill of each bank not yet paid (ADR 0013).
- An account's page shows where each linked line's money went or came from.

### The owner's details on an account

An account takes two details from the owner, both optional (`AGENTS.md`): a name of their own, shown in place of the bank's (`accounts.name`, ADR 0013), and its full number (`accounts.full_number`), shown in place of the masked one and used to fit counterparties. A full number must fit the masked number the bank prints, so a typo is refused. The account is still found by its masked number: a statement prints nothing else.

## Alternatives considered

- **Link by amount and date alone.** Links every card payment found, but also any two equal amounts in two accounts within a few days.
- **Match bills by wording** (卡款, 卡費). Each bank words it its own way (玉山卡款扣繳, 全國繳費網信, 媒體轉帳 with a 台新卡費 note); the amount and the bill's payment line are what they share.
- **Have the owner mark bills paid.** A chore the deposit statements already answer.

## Consequences

- Until a deposit statement covering a bill's due date arrives, the bill is not known to be paid and counts as owed.
- A bank whose deposit statements Finchie does not read (the account a card is paid from) leaves its card payments unlinked; its bills are paid once a later bill shows the payment.
