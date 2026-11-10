"""Taishin's combined statement (綜合對帳單): its deposit accounts and their lines (ADR 0013).

Page 1 prints the period. The account summaries (帳戶概要區) list each NT$ and foreign currency account
with its balance; a foreign currency account's currency sits half a row above it and its exchange rate
half a row below. The tables of lines (往來明細) carry the account number on every row and may run onto
the next page. Everything after page 1 is read by position: a withdrawal and a deposit are told apart
only by their column, and an interest line can print an amount in both.
"""

import re
from collections.abc import Sequence
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import DepositLine
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.deposit import Accounts, amounts_by_column, moved_in
from finchie_parser.parsers.kinds import deposit_kind
from finchie_parser.parsers.layout import nearest_column, rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import AMOUNT, CURRENCY, ISO_DATE, MASKED_ACCOUNT, amount, clean, find, iso_date, token

_PERIOD = re.compile(rf"資料期間：({ISO_DATE})~({ISO_DATE})", re.ASCII)
_ACCOUNT = token(MASKED_ACCOUNT)
_DATE = token(ISO_DATE)
# Amounts carry a dollar sign, after the minus if any: -$1,234.
_MONEY = token(rf"-?\$({AMOUNT})")
_CURRENCY = token(CURRENCY)
# Words only the account summaries' heading rows print.
_SUMMARY_HEADINGS = frozenset({"帳號類別", "(原幣)", "(新臺幣)", "幣別/匯率"})


def _money(text: str) -> Decimal:
    return amount(text.replace("$", ""))


class TaishinDepositParser(StatementParser):
    name = "taishin.deposit"
    version = "2"
    institution = "taishin"
    # 台新銀行綜合對帳單 2026年9月份; the card bill comes from the same address.
    sender_domains = ("taishinbank.com.tw",)
    subject_terms = ("綜合對帳單",)
    filename_pattern = re.compile(r"^電子對帳單\.pdf$")
    text_marker = "資料期間"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 加密型帳單輸入密碼為『身分證號』( 第一碼英文字母須大寫 ).
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        period = find(_PERIOD, document.page_texts(1), "statement period")
        reader = _Reader()
        for page in range(1, document.page_count()):
            reader.read_page(document.page_words(page))
        start, end = iso_date(period.group(1)), iso_date(period.group(2))
        # No opening balance: each account is reconciled from before its first line.
        return Parsed(reader.accounts.statement(self.institution, start, end, "帳戶餘額"))


class _Reader:
    """Walks the pages' rows, section by section; a table's headings carry over a page break."""

    def __init__(self) -> None:
        self.accounts = Accounts()
        self.section = ""
        self.headings: dict[str, Word] = {}

    def read_page(self, words: Sequence[Word]) -> None:
        for row in rows_of(words):
            texts = [word.text for word in row]
            title = texts[0]
            if title in ("新臺幣帳戶概要區", "外幣帳戶概要區", "新臺幣帳戶的往來明細", "外幣帳戶的往來明細"):
                self.section, self.headings = title, {}
            elif title in ("各產品訊息區", "您在本行的交易往來"):
                self.section, self.headings = "", {}
            elif _SUMMARY_HEADINGS & set(texts) or ("帳號" in texts and "帳戶餘額" in texts):
                # A summary's headings span up to three printed rows; keep every one seen.
                self.headings |= {word.text: word for word in row}
            elif self.section.endswith("概要區") and len(texts) > 1 and _ACCOUNT.fullmatch(texts[1]):
                self._summary(row, words)
            elif (
                self.section.endswith("往來明細")
                and _ACCOUNT.fullmatch(title)
                and len(texts) > 1
                and _DATE.fullmatch(texts[1])
            ):
                self._line(row)

    def _summary(self, row: list[Word], page: Sequence[Word]) -> None:
        kind, number = row[0].text, row[1].text
        balances = [word for word in row if _MONEY.fullmatch(word.text)]
        notes = (
            [word.text for word in row[2:] if not _MONEY.fullmatch(word.text) and word.x0 > balances[-1].x1]
            if balances
            else []
        )
        name = " ".join([kind, *notes])
        if self.section == "新臺幣帳戶概要區":
            balance = self._column(balances, "帳戶餘額", number)
            self.accounts.add(number, "TWD", name, balance)
            return
        # The original currency's balance, not its NT$ worth; the currency half a row above.
        original = self._column(balances, "(原幣)", number)
        heading = self.headings.get("幣別/匯率")
        if heading is None:
            raise LayoutError("foreign currency summary without its 幣別/匯率 heading")
        currencies = [
            word.text
            for word in page
            if _CURRENCY.fullmatch(word.text)
            and abs(word.top - row[0].top) <= 10
            and heading.x0 - 15 <= word.x0 <= heading.x1 + 15
        ]
        if len(currencies) != 1:
            raise LayoutError(f"{number}: no single currency beside it ({currencies})")
        self.accounts.add(number, currencies[0], name, original)

    def _column(self, amounts: list[Word], heading: str, number: str) -> Decimal:
        column = self.headings.get(heading)
        if column is None or not amounts:
            raise LayoutError(f"{number}: no balance under {heading}")
        return _money(min(amounts, key=lambda word: abs(word.x1 - column.x1)).text)

    def _line(self, row: list[Word]) -> None:
        columns: dict[str, Word] = {
            name: self.headings[name] for name in ("支出金額", "存入金額", "帳戶餘額") if name in self.headings
        }
        if len(columns) != 3 or "日期" not in self.headings or "備註" not in self.headings:
            raise LayoutError(f"{row[0].text}: a line before its table's headings")
        number, when, rest = row[0].text, row[1], row[2:]
        currency = "TWD"
        if self.section == "外幣帳戶的往來明細":
            column = self.headings.get("幣別")
            found = [w.text for w in rest if column and _CURRENCY.fullmatch(w.text) and abs(w.x0 - column.x0) <= 15]
            if len(found) != 1:
                raise LayoutError(f"{number} {when.text}: no currency")
            currency = found[0]
        account = self.accounts.get(number, currency)
        if account is None:
            raise LayoutError(f"lines for {number} {currency}, which the summaries do not list")

        by_column, text = amounts_by_column(
            rest,
            lambda word: nearest_column(word, columns) if _MONEY.fullmatch(word.text) else None,
            f"{number} {when.text}",
            _money,
        )
        if "帳戶餘額" not in by_column:
            raise LayoutError(f"{number} {when.text}: no balance")
        # The description sits between the date and the next column (支票號碼, or 幣別 for foreign currency).
        next_column = self.headings.get("幣別") or self.headings.get("支票號碼") or columns["支出金額"]
        description = clean(" ".join(w.text for w in text if w.x1 <= next_column.x0))
        memo = clean(" ".join(w.text for w in text if w.x0 > columns["帳戶餘額"].x1))
        account.lines.append(
            DepositLine(
                date=iso_date(when.text),
                description=description,
                amount=moved_in(by_column, "存入金額", "支出金額"),
                kind=deposit_kind(description),
                memo=memo,
                balance=by_column["帳戶餘額"],
            )
        )
