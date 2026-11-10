"""Taipei Fubon's bank statement (銀行對帳單): its deposit accounts and their lines (ADR 0013).

Page 1 prints the period. 資產持有明細 lists each deposit account with its balance; 當月交易明細 lists each
account's lines, opening with the balance brought forward (承轉結餘), the account number on that row only.
Headings are centred over their columns, so a word belongs to the heading whose centre is nearest.
"""

import re
from collections.abc import Sequence

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositLine
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.deposit import Account, Accounts, amounts_by_column, moved_in
from finchie_parser.parsers.kinds import deposit_kind
from finchie_parser.parsers.layout import nearest_centre, rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import CENTS, CURRENCY, ISO_DATE, MASKED_ACCOUNT, amount, clean, find, iso_date, token

_PERIOD = re.compile(rf"對帳單期間：({ISO_DATE})~({ISO_DATE})", re.ASCII)
_ACCOUNT = token(MASKED_ACCOUNT)
_DATE = token(ISO_DATE)
# Two decimals: other numbers on a row are not amounts.
_AMOUNT = token(CENTS)
_CURRENCY = token(CURRENCY)
_BROUGHT_FORWARD = "承轉結餘"
_AMOUNT_COLUMNS = ("支出", "收入", "餘額")


class FubonDepositParser(StatementParser):
    name = "fubon.deposit"
    version = "2"
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
        period = find(_PERIOD, document.page_texts(1), "statement period")
        reader = _Reader()
        for page in range(1, document.page_count()):
            reader.read_page(document.page_words(page))
        start, end = iso_date(period.group(1)), iso_date(period.group(2))
        return Parsed(reader.accounts.statement(self.institution, start, end, "餘額"))


class _Reader:
    def __init__(self) -> None:
        self.accounts = Accounts()
        self.section = ""
        self.headings: dict[str, Word] = {}
        self.current: Account | None = None

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
            elif self.section == "資產持有明細" and self.headings and any(_ACCOUNT.fullmatch(t) for t in texts):
                self._holding(row)
            elif self.section == "當月交易明細" and self.headings and any(_DATE.fullmatch(t) for t in texts[:2]):
                self._line(row)

    def _holding(self, row: list[Word]) -> None:
        number = next(w.text for w in row if _ACCOUNT.fullmatch(w.text))
        currencies = [w.text for w in row if _CURRENCY.fullmatch(w.text)]
        balances = [w for w in row if _AMOUNT.fullmatch(w.text) and self._column(w) == "餘額"]
        if len(currencies) != 1 or len(balances) != 1:
            raise LayoutError(f"{number}: expected one currency and one balance")
        self.accounts.add(number, currencies[0], row[0].text, amount(balances[0].text))

    def _line(self, row: list[Word]) -> None:
        if _ACCOUNT.fullmatch(row[0].text):
            # The balance brought forward opens an account's lines, its number on that row only.
            found = self.accounts.get(row[0].text, "TWD")
            if found is None:
                raise LayoutError(f"lines for {row[0].text}, which the holdings do not list")
            self.current, row = found, row[1:]
        if self.current is None:
            raise LayoutError(f"{row[0].text}: a line before its account")
        when, rest = row[0], row[1:]
        by_column, notes = amounts_by_column(rest, self._amount_column, when.text)
        if "餘額" not in by_column:
            raise LayoutError(f"{when.text}: no balance")
        description = [w.text for w in notes if self._column(w) in ("日期", "摘要")]
        if description == [_BROUGHT_FORWARD]:
            self.current.opening = by_column["餘額"]
            return
        # An account number printed past the amounts: the other side of a transfer.
        counterparty = [w.text for w in notes if self._column(w) not in ("日期", "摘要")]
        text = clean(" ".join(description))
        self.current.lines.append(
            DepositLine(
                date=iso_date(when.text),
                description=text,
                amount=moved_in(by_column, "收入", "支出"),
                kind=deposit_kind(text),
                counterparty=" ".join(counterparty),
                balance=by_column["餘額"],
            )
        )

    def _column(self, word: Word) -> str:
        # Headings are centred over their columns.
        return nearest_centre(word, self.headings)

    def _amount_column(self, word: Word) -> str | None:
        column = self._column(word)
        return column if _AMOUNT.fullmatch(word.text) and column in _AMOUNT_COLUMNS else None
