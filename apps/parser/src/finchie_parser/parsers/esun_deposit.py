"""E.SUN's combined statement (綜合對帳單): its deposit accounts and their lines (ADR 0013).

Page 1 prints the period; page 2 lists every deposit account with its balance, then the lines of each
account that had any, in a table whose withdrawal and deposit columns are read by position: only one of
them holds an amount, and the text alone does not say which.
"""

import re
from collections.abc import Sequence

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositLine
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.deposit import Accounts, amount_and_balance
from finchie_parser.parsers.kinds import deposit_kind
from finchie_parser.parsers.layout import rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import (
    CENTS,
    CURRENCY,
    ISO_DATE,
    MASKED_ACCOUNT,
    ROC_DATE,
    amount,
    clean,
    find,
    iso_date,
    roc_date,
    token,
)

_PERIOD = re.compile(rf"對帳單期間：({ISO_DATE})－({ISO_DATE})", re.ASCII)
_BALANCES_HEADER = "項目 銀行帳號 幣別 存款餘額(元)"
_BALANCE = re.compile(rf"^(\S+) ({MASKED_ACCOUNT}) ({CURRENCY}) ({CENTS})$", re.ASCII)
# Two decimals: other numbers on a row, such as the other side's bank code, are not amounts.
_AMOUNT = token(CENTS)
_ROC_DATE = token(ROC_DATE)
_COLUMNS = ("交易日期", "摘要", "提款", "存款", "轉入帳戶/備註", "對方帳號", "餘額")


class EsunDepositParser(StatementParser):
    name = "esun.deposit"
    version = "2"
    institution = "esun"
    # 玉山銀行2026年09月綜合對帳單; the card bill comes from the same address.
    sender_domains = ("esunbank.com",)
    subject_terms = ("綜合對帳單",)
    filename_pattern = re.compile(r"^玉山銀行綜合對帳單\.pdf$")
    text_marker = "對帳單期間"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 開啟檔案的密碼為您的身分證字號或統一編號，英文請輸入大寫.
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        period = find(_PERIOD, texts[:1], "statement period")
        start, end = iso_date(period.group(1)), iso_date(period.group(2))

        accounts = _balances(texts)
        for number, lines in _lines(document, len(texts)).items():
            match accounts.numbered(number):
                case []:
                    raise LayoutError(f"lines for {number}, which the balances do not list")
                case [account]:
                    account.lines = lines
                case _:
                    # A foreign currency account holding several currencies would need the table's currency.
                    raise LayoutError(f"lines of {number}, which holds several currencies")
        # No opening balance: each account is reconciled from before its first line.
        return Parsed(accounts.statement(self.institution, start, end, "存款餘額"))


def _balances(texts: Sequence[str]) -> Accounts:
    """Each account and currency the 存款 section lists, with its name and balance, in order."""
    for text in texts:
        rows = text.splitlines()
        if _BALANCES_HEADER not in rows:
            continue
        accounts = Accounts()
        for row in rows[rows.index(_BALANCES_HEADER) + 1 :]:
            match = _BALANCE.match(row)
            if not match:
                break
            name, number, currency, balance = match.groups()
            accounts.add(number, currency, name, amount(balance))
        if not accounts:
            raise LayoutError("the deposit section lists no account")
        return accounts
    raise LayoutError("deposit section not found")


def _lines(document: Document, pages: int) -> dict[str, list[DepositLine]]:
    """Every account's lines, in printed order. A 銀行帳號 row starts an account's table, a header row
    says where its columns are, and each row after it that starts with a date is a line."""
    by_account: dict[str, list[DepositLine]] = {}
    number = ""
    columns: dict[str, Word] = {}
    for page in range(pages):
        for row in rows_of(document.page_words(page)):
            texts = [word.text for word in row]
            if texts[0] == "銀行帳號" and len(texts) == 2:
                number, columns = texts[1], {}
            elif all(column in texts for column in _COLUMNS):
                columns = {word.text: word for word in row if word.text in _COLUMNS}
            elif _ROC_DATE.fullmatch(texts[0]) and number and columns:
                by_account.setdefault(number, []).append(_line(row, columns))
    return by_account


def _line(row: list[Word], columns: dict[str, Word]) -> DepositLine:
    when, rest = row[0], row[1:]
    amounts = [word for word in rest if _AMOUNT.fullmatch(word.text)]
    # Amounts are right-aligned under their column's heading.
    withdrawal = columns["提款"]
    value, balance = amount_and_balance(amounts, withdrawal, columns["存款"], when.text)

    text = [word for word in rest if word not in amounts]
    description = [word.text for word in text if word.x1 <= withdrawal.x0]
    notes = [word for word in text if word.x1 > withdrawal.x0]
    # 對方帳號 (bank code and account) starts left of its heading; 轉入帳戶/備註 well right of its own.
    split = (columns["轉入帳戶/備註"].x0 + columns["對方帳號"].x0) / 2
    text_description = clean(" ".join(description))
    return DepositLine(
        date=roc_date(when.text),
        description=text_description,
        amount=value,
        kind=deposit_kind(text_description),
        memo=clean(" ".join(word.text for word in notes if word.x0 < split)),
        counterparty=" ".join(word.text for word in notes if word.x0 >= split),
        balance=balance,
    )
