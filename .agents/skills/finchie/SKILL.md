---
name: finchie
description: Answer questions about the owner's real finances, and analyse them, through Finchie's CLI. Finchie is their self-hosted ledger, built from bank and card statements and exchanges' read-only keys - deposit, card, securities, exchange, and points accounts, every transaction, card bills, and net worth over time. Use when the owner asks about their spending, income, balances, bills, transactions, investments, or net worth.
---

# Finchie

Finchie is the owner's own ledger. Its CLI makes every operation of the API a command, built at each run from what the deployment serves:

```sh
uvx --from "git+https://github.com/hsin19/finchie#subdirectory=apps/cli" finchie <command> [options]
```

Below, `finchie` stands for that whole line. Write it out in full in every shell call: an alias or a shell function does not outlive one call.

## Before the first call

The CLI reads where Finchie is and the agents' service token from `~/.config/finchie/config.toml`, or from `FINCHIE_URL`, `FINCHIE_CLIENT_ID`, and `FINCHIE_CLIENT_SECRET`. When it says they are missing, pass its message on to the owner and stop; don't look for the values elsewhere. Never print the config file: it holds a secret.

## Finding the command

1. `finchie --help` lists every command with its method and what it does.
2. `finchie <command> --help` gives its options and every field it returns, with what each one means. Read it before a command's first call: a field rarely means only what its name says.

The agents' token reads only: commands other than GET answer 403. When something in the ledger should change (a payee's name, an account's details), tell the owner what to change in the web app.

## Analysing

- Save what you need to a file and compute from it, instead of reading rows into the conversation: `finchie search-transactions --from 2026-01-01 --hide-transfers --all > tx.ndjson`. `--all` goes through every page and writes one item per line.
- For a search's totals without its rows, `--limit 0`.
- Amounts are decimal strings: add them as decimals (Python's `decimal.Decimal`), never as floats. Every amount has its `unit` (TWD, USD, a security, a points program); never add across units.
- Spending and income come from `flow`, seen from the owner (money out negative), not from `amount`, as the statement prints it (a card purchase positive).
- When adding up rows yourself, pass `--hide-transfers`: otherwise money moved between the owner's own accounts, and card bills paid from a deposit account, count as spending and income too. With it, a purchase made by card counts once, on the card.
- A search's `flows` already keeps those moves apart (`moved`) from what was `spent` and `received`, per unit. Its `sum` adds amounts as printed and only means something within one `--account-kind`.
- While a search includes purchases, a fee or refund linked to its purchase is folded into it, one of its `children`, and the purchase's amounts are net of them.
- Net worth (`get-net-worth`, `get-net-worth-history`) is in NT$. After `completeAsOf` it is an estimate, and `behind` names the accounts that have not reported since.

## Care

This is the owner's real financial data. Keep the files you write in a temporary directory, send none of it to other services, and put in an answer only what the question needs.
