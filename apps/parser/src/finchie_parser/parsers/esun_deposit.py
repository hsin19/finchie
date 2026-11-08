"""E.SUN's combined statement (綜合對帳單): its deposit accounts and their lines (ADR 0013).

Page 1 prints the period; page 2 lists every deposit account with its balance, then the lines of each
account that had any, in a table whose withdrawal and deposit columns are read by position: only one of
them holds an amount, and the text alone does not say which.
"""

import re
from collections.abc import Sequence
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositAccount, DepositLine, DepositStatement
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser, find, reconcile_account, rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import amount, clean, iso_date, roc_date

_PERIOD = re.compile(r"對帳單期間：(\d{4}/\d\d/\d\d)－(\d{4}/\d\d/\d\d)", re.ASCII)
_BALANCES_HEADER = "項目 銀行帳號 幣別 存款餘額(元)"
_BALANCE = re.compile(r"^(\S+) (\d+\*+\d+) ([A-Z]{3}) (-?[\d,]+\.\d\d)$", re.ASCII)
_AMOUNT = re.compile(r"-?[\d,]+\.\d\d", re.ASCII)
_ROC_DATE = re.compile(r"\d{3}/\d\d/\d\d", re.ASCII)
_COLUMNS = ("交易日期", "摘要", "提款", "存款", "轉入帳戶/備註", "對方帳號", "餘額")


class EsunDepositParser(StatementParser):
    name = "esun.deposit"
    version = "1"
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

        balances = _balances(texts)
        lines = _lines(document, len(texts))
        unknown = set(lines) - {number for number, _ in balances}
        if unknown:
            raise LayoutError(f"lines for accounts the balances do not list: {sorted(unknown)}")

        accounts = []
        for (number, currency), (name, closing) in balances.items():
            own = lines.get(number, [])
            if own and sum(1 for n, _ in balances if n == number) > 1:
                # A foreign currency account holding several currencies would need the table's currency.
                raise LayoutError(f"lines of {number}, which holds several currencies")
            reconciliation = reconcile_account(None, closing, own, "存款餘額")
            accounts.append(DepositAccount(number, name, currency, closing, own, reconciliation))
        return Parsed(DepositStatement(self.institution, start, end, accounts))


def _balances(texts: Sequence[str]) -> dict[tuple[str, str], tuple[str, Decimal]]:
    """Each account and currency the 存款 section lists, with its name and balance, in order."""
    for text in texts:
        rows = text.splitlines()
        if _BALANCES_HEADER not in rows:
            continue
        found: dict[tuple[str, str], tuple[str, Decimal]] = {}
        for row in rows[rows.index(_BALANCES_HEADER) + 1 :]:
            match = _BALANCE.match(row)
            if not match:
                break
            name, number, currency, balance = match.groups()
            found[(number, currency)] = (name, amount(balance))
        if not found:
            raise LayoutError("the deposit section lists no account")
        return found
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
    if len(amounts) != 2:
        raise LayoutError(f"{when.text}: expected an amount and a balance, found {[w.text for w in amounts]}")
    moved, balance = sorted(amounts, key=lambda w: w.x1)
    # Amounts are right-aligned under their column's heading.
    withdrawal, deposit = columns["提款"], columns["存款"]
    out = abs(moved.x1 - withdrawal.x1) < abs(moved.x1 - deposit.x1)
    value = amount(moved.text)

    text = [word for word in rest if word not in amounts]
    description = [word.text for word in text if word.x1 <= withdrawal.x0]
    notes = [word for word in text if word.x1 > withdrawal.x0]
    # 對方帳號 (bank code and account) starts left of its heading; 轉入帳戶/備註 well right of its own.
    split = (columns["轉入帳戶/備註"].x0 + columns["對方帳號"].x0) / 2
    return DepositLine(
        date=roc_date(when.text),
        description=clean(" ".join(description)),
        amount=-value if out else value,
        memo=clean(" ".join(word.text for word in notes if word.x0 < split)),
        counterparty=" ".join(word.text for word in notes if word.x0 >= split),
        balance=amount(balance.text),
    )
