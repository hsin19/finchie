# ADR 0019: Points

## Decision

Points and miles are accounts (ADR 0013) of kind `points`: a bank's card rewards, one account per bank, and later programs the owner keeps by hand. Each program is a unit of its own, named as its source prints it (`小樹點(信用卡)`, `e point`, `台新Point(信用卡)`, `滙豐旅遊積分`): one program's points never add up with another's (ADR 0013), and a printed name says which program it is.

### From card bills

All four banks print their programs on the bill, in a table of their own beside the charges.

| Bank | Program | Opening | Added | Used | Closing | Expiring |
|---|---|---|---|---|---|---|
| Cathay | 小樹點(信用卡) | 上期累計 | 本期新增/扣除, 本期贈送 | 本期兌/轉換 (printed positive) | 本期結餘 | This month's (by the closing month's last day), and the next twelve months' together, this month's included (by the date printed) |
| E.SUN | e point | 上期累積點數 | 本期加點, 本期調整點數 (gone from the October 2026 bill) | 本期扣點 | 本期結餘點數 | Per month (by the month's last day) |
| Taishin | 台新Point(信用卡); 大集大利點數 until 2022-06 | 上期結餘點數/里數 | 新增回饋, 活動回饋/調整 | 本期使用點數/里數 (printed negative) | 本期結餘回饋 | One amount, on the date printed |
| HSBC | 滙豐旅遊積分 | 上月結餘積分 | 本期新增積分 | 本期使用積分(含調整) | 本期積分總結餘 | 次月到期點數 (by next month's last day) |

Cathay's and E.SUN's headings are images or unmapped glyphs, and HSBC draws every label as an image; their columns were read off the rendered page, and HSBC's figures are read by position. Taishin's table has a column for 亞洲萬里通里數 too, read as a program of its own where the bill prints a figure in it (`**` where the card earns none).

- **The parser** reads each program's opening, its period's movements, and its closing, and checks that they add up: opening plus the movements gives closing. Points used are taken by their size, since one bank prints them positive and another negative. A program that does not add up is a warning on the document; its closing stays as printed (ADR 0005).
- **Lines** are the period's movements as the bill totals them, one per column with a figure, under the bill's own label: `earn` (a bonus included), `adjust`, or `redeem` (negative), dated on the closing date. Search lists them; like trades (ADR 0017) they are neither spent nor received.
- **Balances**: each program's closing is the balance of its unit in the bank's points account on the closing date.
- **Continuity** is checked (ADR 0013). A bill lists every movement but points that lapse, which show nowhere but in the next opening (Cathay's drops by the month's expiring points). So a gap in a points account is points lost, which is worth knowing; making up for it by hand comes later.
- **Expiries** are what a source says will lapse: an amount and the date by which it lapses, as printed (the table's last column). A unit's newest report of them stands: the newest statement that reports the unit at all, so a bill that prints nothing lapsing says nothing will. Amounts of 0 and dates gone by in Taipei are not shown.

### By hand

Some of what the owner holds no source reports: a hotel's points, an airline's miles, and with them cash or a broker that sends nothing. The owner keeps such an account by hand. It is an account like any other (ADR 0013), whose source is the owner; manual input stays a supplement, never what a feature depends on (`AGENTS.md`).

- **Opening**: of any kind but a card's, which only holds its bank's bills' lines (ADR 0013); institution `owner`, and the name the owner opens it with as its own, its `label`, as a bank's account has the bank's. Nothing else to begin with: no number, no unit.
- **What it holds** is the owner's report of the whole account on the day, Taipei's, a snapshot like an exchange's (ADR 0018): any number of units, each of the kind the account holds (money, securities by name, points; ADR 0020). A later report the same day replaces the day's, and a unit held before and no longer listed is at 0. Of points it also says what lapses by when, which replaces all the account had.
- **Like any account**: its worth is its units' at their newest prices; the owner may give it another name and a full number (ADR 0016), and clearing the name brings its own back. No source prints its number, so the full number is whatever the owner types, such as a membership number.
- **Telling accounts apart**: a source's accounts stay one per institution, kind, and number, so that each report finds its own. The owner's are found by their ids, as many as the owner likes, several of one program included, such as two members'. No two accounts go by one name the owner gave, a source's included: the name they gave it, else its own where they opened it. Names are how the owner tells accounts apart; a bank's own, such as two of its accounts both 外幣活存, may repeat. Names given twice before this keep the first; the others get their id. Nothing in the code names a program.
- **Continuity** is checked only for balances statements report: snapshots, an exchange's or the owner's, have no lines to join them, so an account with nothing else is unknown.
- **What a point is worth** is a price like any other unit's (ADR 0020): the owner gives one, which counts while it is the newest.

### Worth

A program has no exchange rate: a points account has no worth until its program has a price (ADR 0020), and until then its units are not hidden as small. The owner counts Cathay's, E.SUN's, and Taishin's card points at NT$1 each, which the migration gives them.

### Next

- **Making up a gap by hand**, as lapsed points, and **reminders** of points about to lapse, are left for later. (Filling a gap by hand came later, for any itemized account: a line of kind `fill`, migration 00021.)
- **Working out what lapses when**, ahead of the bills, is left for later too: points as lots, each with a date it lapses by (as a source reports it, by a program's rule, or as the owner enters it), used oldest first. Cathay's bills already allow it: each month's twelve-month total, less the points used, grows by the month that enters the window, which gives every month's lapsing points a year ahead. Worked out from the owner's bills, it matched what each of the 38 bills said would lapse that month.
- **A program's rule** would be the unit's, not an account's or a balance's: points earned in a period lapse together a fixed time later (Cathay's apparently by the end of the 35th month after the one they were earned in, to be confirmed against its terms), or all at once a while after the account last moved (common for hotels and airlines). A bank's rule would come from its parser, the owner's be set once. Each lot's date would be worked out on every read, never stored, so a rule set or corrected later applies to every line; what a bill prints would check it, a mismatch a warning like an unbalanced statement.

## Alternatives considered

- **No lines, only balances.** Simpler, but what was earned and used each period would show nowhere: a change in the balance mixes them with lapsed points.
- **Lapsed points as lines**, worked out from the gap. They are what the gap already shows; a line Finchie makes up is not one a source reported.
- **A unit per bank** (`CATHAY_POINTS`). Stable across renames, but a code Finchie makes up, where the bill names the program.
- **An expiry rule per program** (ADR 0013's rolling and fixed). The bills print what lapses when; Finchie needs no rule to work it out.
- **A points account per program kept by hand, its number the program** (as first built). A wallet of cash in two currencies, or a member's account with two programs, would be several accounts, and the number, which could hold a membership number, would hold a unit's name.
- **Only points kept by hand.** The owner keeps more than points by hand: cash, a broker without statements.
- **Names unique among the owner's accounts only.** A rule for one kind of account, when a name shared with a bank's account confuses as much.
- **Each unit reported on its own.** A whole snapshot is what exchanges report already: one rule for a unit taken off, and one report a day.

## Consequences

- `accounts.kind` gains `points`, transactions the kinds `earn`, `adjust`, and `redeem`, and the backend the `expiries` table. The card `Statement` in `parser.proto` gains `points`.
- The API gains `POST /api/accounts` (kind and name) and `PUT /api/accounts/{id}/holdings`; `PATCH /api/accounts/{id}` answers `409` for a name another account has. Accounts are still never deleted (ADR 0013), the owner's included.
- The accounts' institution, kind, and number are unique only for sources' accounts, and their names for all.
- Bills stored before this get their points when re-parsed.
- ADR 0013's `accounts.expiry` stays `none`: expiries come as reported, not by rule.
