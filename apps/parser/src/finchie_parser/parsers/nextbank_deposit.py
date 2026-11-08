"""Next Bank's combined statement (綜合對帳單): its accounts and their lines (ADR 0013).

Page 1 prints the period. Every account and currency then has a section of its own (帳號：… ｜ USD ｜
美元餘額：…) that prints its balance in its heading, then, if it had any lines, the balance brought forward
(上期餘額) and the lines, dated by month and day only, 收入 before 支出. Pockets (活存口袋) follow, each with
a balance of its own.
"""

import re
from collections.abc import Sequence
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositAccount, DepositKind, DepositLine, DepositStatement
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import (
    Parsed,
    StatementParser,
    find,
    nearest_column,
    reconcile_account,
    rows_of,
)
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import amount, clean, iso_date

_PERIOD = re.compile(r"帳單期間 (\d{4}/\d\d/\d\d)~(\d{4}/\d\d/\d\d)", re.ASCII)
_HEADING_BALANCE = re.compile(r"餘額：(?:NT\$)?(-?[\d,]+(?:\.\d+)?)$")
_CURRENCY = re.compile(r"[A-Z]{3}", re.ASCII)
_MONTH_DAY = re.compile(r"(\d{1,2})/(\d{1,2})", re.ASCII)
_AMOUNT = re.compile(r"-?[\d,]+(?:\.\d+)?", re.ASCII)
_COLUMNS = ("收入", "支出", "餘額")
# The bank's own words for interest.
_INTEREST = frozenset({"利息存入"})


@dataclass
class _Account:
    name: str
    closing: Decimal
    opening: Decimal | None = None
    lines: list[DepositLine] = field(default_factory=list)


class NextBankDepositParser(StatementParser):
    name = "nextbank.deposit"
    version = "1"
    institution = "nextbank"
    # 【將來銀行通知】2026年9月綜合對帳單, from no_reply@eblsdr.nextbank.com.tw (the e-mail asks to trust
    # eStatement@nextbank.com.tw).
    sender_domains = ("nextbank.com.tw",)
    subject_pattern = re.compile(r"綜合對帳單")
    filename_pattern = re.compile(r"^(NEXTBANK_)?綜合對帳單_\d{4}_\d{1,2}\.pdf$")
    text_marker = "帳單期間"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 輸入你的身分證字號（英文字母請大寫）.
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        period = find(_PERIOD, texts[:1], "statement period")
        start, end = iso_date(period.group(1)), iso_date(period.group(2))
        reader = _Reader(start, end)
        for page in range(1, len(texts)):
            reader.read_page(document.page_words(page))
        if not reader.accounts:
            raise LayoutError("no account section found")

        accounts = []
        for (number, currency), found in reader.accounts.items():
            reconciliation = reconcile_account(found.opening, found.closing, found.lines, "餘額")
            accounts.append(
                DepositAccount(number, found.name, currency, found.closing, found.lines, reconciliation, found.opening)
            )
        return Parsed(DepositStatement(NextBankDepositParser.institution, start, end, accounts))


class _Reader:
    def __init__(self, start: date, end: date) -> None:
        self.start, self.end = start, end
        self.accounts: dict[tuple[str, str], _Account] = {}
        self.kind = ""
        self.current: _Account | None = None
        self.headings: dict[str, Word] = {}
        self.pocket_name = ""

    def read_page(self, words: Sequence[Word]) -> None:
        for row in rows_of(words):
            texts = [word.text for word in row]
            first = texts[0]
            if first in ("新臺幣帳戶明細", "外幣帳戶明細"):
                self.kind, self.current = first.removesuffix("明細"), None
            elif first == "活存口袋明細":
                self.kind, self.current = "口袋", None
            elif first == "注意事項":
                self.kind, self.current = "", None
            elif first.startswith("帳號：") and self.kind:
                currencies = [t for t in texts if _CURRENCY.fullmatch(t)]
                if len(currencies) != 1:
                    raise LayoutError(f"{first}: no single currency in its heading")
                self._section(first.removeprefix("帳號："), currencies[0], texts[-1], self.kind)
            elif first.startswith("序號：") and self.kind == "口袋":
                # A pocket holds NT$ (餘額：NT$…).
                self._section(first.removeprefix("序號："), "TWD", texts[-1], self.pocket_name or "口袋")
            elif self.kind == "口袋" and len(texts) == 1 and first not in ("本月無交易紀錄",):
                # A pocket's name heads its section.
                self.pocket_name = first
            elif "交易日期" in texts and all(column in texts for column in _COLUMNS):
                self.headings = {word.text: word for word in row}
            elif first == "上期餘額" and self.current is not None:
                self.current.opening = amount(texts[-1])
            elif _MONTH_DAY.fullmatch(first) and self.current is not None:
                if self.kind == "口袋":
                    raise LayoutError("a pocket's lines: a layout not seen yet")
                self.current.lines.append(self._line(row))

    def _section(self, number: str, currency: str, balance_text: str, name: str) -> None:
        balance = _HEADING_BALANCE.search(balance_text)
        if balance is None:
            raise LayoutError(f"{number}: section heading without its balance")
        if (number, currency) in self.accounts:
            raise LayoutError(f"{number} {currency} listed twice")
        self.current = self.accounts[(number, currency)] = _Account(name, amount(balance.group(1)))

    def _line(self, row: list[Word]) -> DepositLine:
        columns = {name: self.headings[name] for name in _COLUMNS if name in self.headings}
        if len(columns) != len(_COLUMNS):
            raise LayoutError(f"{row[0].text}: a line before its table's headings")
        when, rest = row[0], row[1:]
        by_column: dict[str, Decimal] = {}
        text: list[Word] = []
        for word in rest:
            if _AMOUNT.fullmatch(word.text) and word.x0 > columns["收入"].x0 - 30:
                column = nearest_column(word, columns)
                if column in by_column:
                    raise LayoutError(f"{when.text}: two amounts under {column}")
                by_column[column] = amount(word.text)
            else:
                text.append(word)
        if "餘額" not in by_column:
            raise LayoutError(f"{when.text}: no balance")
        description = clean(" ".join(w.text for w in text if w.x1 < columns["收入"].x0))
        return DepositLine(
            date=self._date(when.text),
            description=description,
            amount=by_column.get("收入", Decimal(0)) - by_column.get("支出", Decimal(0)),
            kind=DepositKind.INTEREST if description in _INTEREST else DepositKind.OTHER,
            memo=clean(" ".join(w.text for w in text if w.x0 > columns["餘額"].x1)),
            balance=by_column["餘額"],
        )

    def _date(self, month_day: str) -> date:
        """`9/21` as the day within the statement's period, which may span a year end."""
        month, day = (int(part) for part in month_day.split("/"))
        for year in range(self.start.year, self.end.year + 1):
            candidate = date(year, month, day)
            if self.start <= candidate <= self.end:
                return candidate
        raise LayoutError(f"{month_day} is outside the statement period")
