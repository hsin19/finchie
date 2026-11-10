"""Cathay United Bank's e-passbook (電子存摺): a deposit account and its lines (ADR 0013).

Cathay's monthly bank statement e-mail (綜合對帳單) carries only a link to online banking, with no PDF or
numbers. Instead, the CUBE app exports an e-passbook PDF by e-mail (更多 → 臺幣 → 匯出臺幣電子存摺).

Page 1 is the passbook cover (存摺封面) with the account number, type, and currency. Each following page
prints up to 22 passbook lines: a line's main row holds its line number, date, withdrawal or deposit, and
running balance, with optional remarks (備註) half a row above and the description (說明) and counterparty
details (交易資訊) half a row below.
"""

import re
from collections.abc import Sequence
from dataclasses import dataclass, replace

from finchie_parser.errors import LayoutError
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.deposit import Accounts, Entry, amount_and_balance
from finchie_parser.parsers.kinds import deposit_kind
from finchie_parser.parsers.layout import rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import AMOUNT, ISO_DATE, clean, find, iso_date, token

_ACCOUNT = re.compile(r"帳號 \| (\d[\d-]+) 類別 \| (\S+)")
_PAGE_ACCOUNT = re.compile(r"帳號：(\d[\d-]+)")
_LINE_NO = token(r"\d{1,2}")
_DATE = token(ISO_DATE)
_AMOUNT = token(AMOUNT)
_COUNTERPARTY = re.compile(r"^\((\d{3})\)(\d[\d*]+)(.*)$")
_COLUMNS = ("行次", "日期", "提出", "存入", "餘額")
_SUBROW_REACH = 10.0


@dataclass
class _Entry:
    top: float
    line: Entry


class CathayDepositParser(StatementParser):
    name = "cathay.deposit"
    version = "3"
    institution = "cathay"
    # 【國泰世華銀行】電子存摺, from service@pxbillrc01.cathaybk.com.tw.
    sender_domains = ("cathaybk.com.tw",)
    subject_terms = ("電子存摺",)
    filename_pattern = re.compile(r"^國泰世華電子存摺\.pdf$")
    text_marker = "開戶日期："

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 開啟電子存摺後，輸入您的 身分證字號(英文字母為大寫).
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        if not texts or "臺幣存款" not in texts[0]:
            raise LayoutError("currency not found on passbook cover")
        currency = "TWD"
        header = find(_ACCOUNT, texts[:1], "account number and type")
        number, account_name = header.group(1), clean(header.group(2))

        lines: list[Entry] = []
        for page in range(1, len(texts)):
            for page_number in _PAGE_ACCOUNT.findall(texts[page]):
                if page_number != number:
                    raise LayoutError(f"page {page + 1} is for {page_number}, cover is {number}")
            lines.extend(_read_page(document.page_words(page)))
        if not lines:
            raise LayoutError("passbook has no transactions")

        first, last = lines[0], lines[-1]
        if first.balance is None or last.balance is None:
            raise LayoutError("a passbook line without its balance")
        accounts = Accounts()
        # The passbook prints no balance brought forward: the one before its first line is it.
        account = accounts.add(number, currency, account_name, last.balance)
        account.opening = first.balance - first.amount
        account.lines = lines
        return Parsed(accounts.statement(self.institution, first.date, last.date, "餘額"))


def _read_page(words: Sequence[Word]) -> list[Entry]:
    columns: dict[str, Word] = {}
    info_x0: float | None = None
    entries: list[_Entry] = []
    subrows: list[tuple[float, list[Word]]] = []

    for row in rows_of(words):
        texts = [w.text for w in row]
        if all(col in texts for col in _COLUMNS):
            columns = {w.text: w for w in row if w.text in _COLUMNS}
            continue
        if "說明" in texts and "交易資訊" in texts:
            info = next(w for w in row if w.text == "交易資訊")
            info_x0 = info.x0 - 45
            continue
        if not columns:
            continue

        if len(row) >= 3 and _LINE_NO.fullmatch(row[0].text) and _DATE.fullmatch(row[1].text):
            entries.append(_main_row(row, columns))
        elif len(row) == 1 and row[0].text.isdigit() and row[0].x0 > 500:
            # The page number at the bottom-right corner.
            continue
        elif row[0].x0 > columns["餘額"].x1:
            subrows.append((row[0].top, row))

    if not entries:
        return []
    split_x = info_x0 if info_x0 is not None else columns["餘額"].x1 + 80
    _attach_subrows(entries, subrows, split_x)
    return [entry.line for entry in entries]


def _main_row(row: list[Word], columns: dict[str, Word]) -> _Entry:
    when = row[1]
    amounts = [w for w in row[2:] if _AMOUNT.fullmatch(w.text)]
    value, balance = amount_and_balance(amounts, columns["提出"], columns["存入"], when.text)
    return _Entry(top=row[0].top, line=Entry(date=iso_date(when.text), description="", amount=value, balance=balance))


def _attach_subrows(entries: list[_Entry], subrows: list[tuple[float, list[Word]]], split_x: float) -> None:
    grouped: dict[int, list[tuple[float, list[Word]]]] = {}
    for top, words in subrows:
        index, nearest = min(enumerate(entries), key=lambda pair: abs(top - pair[1].top))
        if abs(top - nearest.top) > _SUBROW_REACH:
            raise LayoutError(f"row at {top} is not near any passbook line: {[w.text for w in words]}")
        grouped.setdefault(index, []).append((top, words))

    for index, entry in enumerate(entries):
        descriptions: list[str] = []
        memos: list[str] = []
        counterparty = ""
        for top, words in sorted(grouped.get(index, []), key=lambda pair: pair[0]):
            left = clean(" ".join(w.text for w in words if w.x0 < split_x))
            right = clean(" ".join(w.text for w in words if w.x0 >= split_x))
            if top > entry.top:
                if left:
                    descriptions.append(left)
                if right:
                    matched = _COUNTERPARTY.match(right)
                    if matched:
                        code, acct, tail = matched.groups()
                        counterparty = f"{code} {acct}"
                        if tail.strip():
                            memos.append(clean(tail))
                    else:
                        memos.append(right)
            else:
                if right:
                    memos.append(right)

        description = " ".join(descriptions)
        entry.line = replace(
            entry.line,
            description=description,
            kind=deposit_kind(description),
            memo=" ".join(memos),
            counterparty=counterparty,
        )
