"""Taipei Fubon's bank statement (銀行對帳單): its deposit accounts and their lines (ADR 0013).

Page 1 prints the period. 資產持有明細 lists each deposit account with its balance; 當月交易明細 lists each
account's lines, opening with the balance brought forward (承轉結餘), the account number on that row only.
Headings are centred over their columns, so a word belongs to the heading whose centre is nearest.
"""

import re
from collections.abc import Sequence
from dataclasses import dataclass, field
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositAccount, DepositLine, DepositStatement
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser, find, reconcile_account, rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import amount, clean, iso_date

_PERIOD = re.compile(r"對帳單期間：(\d{4}/\d\d/\d\d)~(\d{4}/\d\d/\d\d)", re.ASCII)
_NUMBER = re.compile(r"\d+\*+\d+", re.ASCII)
_DATE = re.compile(r"\d{4}/\d\d/\d\d", re.ASCII)
_AMOUNT = re.compile(r"-?[\d,]+\.\d\d", re.ASCII)
_CURRENCY = re.compile(r"[A-Z]{3}", re.ASCII)
_BROUGHT_FORWARD = "承轉結餘"


def _centre(word: Word) -> float:
    return (word.x0 + word.x1) / 2


def _column(word: Word, headings: dict[str, Word]) -> str:
    return min(headings, key=lambda name: abs(_centre(headings[name]) - _centre(word)))


@dataclass
class _Account:
    name: str
    closing: Decimal
    opening: Decimal | None = None
    lines: list[DepositLine] = field(default_factory=list)


class FubonDepositParser(StatementParser):
    name = "fubon.deposit"
    version = "1"
    institution = "fubon"
    # 台北富邦銀行2026年10月 銀行對帳單
    sender_domains = ("taipeifubon.com.tw",)
    subject_terms = ("銀行對帳單",)
    filename_pattern = re.compile(r"^銀行對帳單\.pdf$", re.IGNORECASE)
    text_marker = "對帳單期間"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: (身分證字號)或與本行約定之密碼 … 英文字母皆需輸入大寫. One the owner agreed with the bank
        # goes in PARSER_PDF_PASSWORDS__FUBON.
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        period = find(_PERIOD, texts[:1], "statement period")
        reader = _Reader()
        for page in range(1, len(texts)):
            reader.read_page(document.page_words(page))
        if not reader.accounts:
            raise LayoutError("no deposit account listed")

        accounts = []
        for (number, currency), found in reader.accounts.items():
            reconciliation = reconcile_account(found.opening, found.closing, found.lines, "餘額")
            accounts.append(
                DepositAccount(number, found.name, currency, found.closing, found.lines, reconciliation, found.opening)
            )
        start, end = iso_date(period.group(1)), iso_date(period.group(2))
        return Parsed(DepositStatement(self.institution, start, end, accounts))


class _Reader:
    def __init__(self) -> None:
        self.accounts: dict[tuple[str, str], _Account] = {}
        self.section = ""
        self.headings: dict[str, Word] = {}
        self.current: _Account | None = None

    def read_page(self, words: Sequence[Word]) -> None:
        for row in rows_of(words):
            texts = [word.text for word in row]
            joined = "".join(texts)
            if joined in ("資產持有明細", "當月交易明細", "訊息公告"):
                self.section, self.headings, self.current = joined, {}, None
            elif self.section == "資產持有明細" and "帳戶種類" in texts:
                self.headings = {word.text: word for word in row}
            elif self.section == "當月交易明細" and "摘要" in texts and "餘額" in texts:
                if "幣別" in texts:
                    raise LayoutError("foreign currency lines: a layout not seen yet")
                self.headings = {
                    word.text: word for word in row if word.text in ("日期", "摘要", "支出", "收入", "餘額")
                }
            elif self.section == "資產持有明細" and self.headings and any(_NUMBER.fullmatch(t) for t in texts):
                self._holding(row)
            elif self.section == "當月交易明細" and self.headings and any(_DATE.fullmatch(t) for t in texts[:2]):
                self._line(row)

    def _holding(self, row: list[Word]) -> None:
        number = next(w.text for w in row if _NUMBER.fullmatch(w.text))
        currencies = [w.text for w in row if _CURRENCY.fullmatch(w.text)]
        balances = [w for w in row if _AMOUNT.fullmatch(w.text) and _column(w, self.headings) == "餘額"]
        if len(currencies) != 1 or len(balances) != 1:
            raise LayoutError(f"{number}: expected one currency and one balance")
        if (number, currencies[0]) in self.accounts:
            raise LayoutError(f"{number} {currencies[0]} listed twice")
        self.accounts[(number, currencies[0])] = _Account(row[0].text, amount(balances[0].text))

    def _line(self, row: list[Word]) -> None:
        if _NUMBER.fullmatch(row[0].text):
            # The balance brought forward opens an account's lines, its number on that row only.
            found = self.accounts.get((row[0].text, "TWD"))
            if found is None:
                raise LayoutError(f"lines for {row[0].text}, which the holdings do not list")
            self.current, row = found, row[1:]
        if self.current is None:
            raise LayoutError(f"{row[0].text}: a line before its account")
        when, rest = row[0], row[1:]
        by_column: dict[str, Decimal] = {}
        notes: list[Word] = []
        for word in rest:
            column = _column(word, self.headings)
            if _AMOUNT.fullmatch(word.text) and column in ("支出", "收入", "餘額"):
                if column in by_column:
                    raise LayoutError(f"{when.text}: two amounts under {column}")
                by_column[column] = amount(word.text)
            else:
                notes.append(word)
        if "餘額" not in by_column:
            raise LayoutError(f"{when.text}: no balance")
        description = [w.text for w in notes if _column(w, self.headings) in ("日期", "摘要")]
        if description == [_BROUGHT_FORWARD]:
            self.current.opening = by_column["餘額"]
            return
        # An account number printed past the amounts: the other side of a transfer.
        counterparty = [w.text for w in notes if _column(w, self.headings) not in ("日期", "摘要")]
        self.current.lines.append(
            DepositLine(
                date=iso_date(when.text),
                description=clean(" ".join(description)),
                amount=by_column.get("收入", Decimal(0)) - by_column.get("支出", Decimal(0)),
                counterparty=" ".join(counterparty),
                balance=by_column["餘額"],
            )
        )
