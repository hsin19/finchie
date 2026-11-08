# ADR 0006: Test data

## Decision

**Real financial documents never enter the repository**, which is public. That covers statements, e-mails, text extracted from them, screenshots, and expected-output files derived from them.

- **Unit tests use synthetic fixtures**: hand-written text that follows a bank's layout, with invented merchants, amounts, card numbers, and names.
- **Real statements are tested locally only.** `FINCHIE_REAL_STATEMENTS_DIR` points to a folder of `.eml` or `.pdf` files (by convention `.local/statements/`, gitignored). (Since ADR 0014, `.eml` files only.) The regression test parses each one and asserts that it reconciles. Without the variable, the test is skipped.
- **PDF passwords come only from environment variables** read by the parser, and are never logged. (Since ADR 0014 the variables hold the owner's details, `PARSER_IDENTITY__*`, which each bank's rule turns into its password.)
- HSBC's OCR path needs real page images, so only the local regression test exercises it.

## Background

Statements reveal spending, travel, card numbers, and the inputs to the PDF passwords. V1 kept one sanitized Taishin fixture in the repo; V2 keeps the rule explicit and adds a way to test against real statements without committing them.
