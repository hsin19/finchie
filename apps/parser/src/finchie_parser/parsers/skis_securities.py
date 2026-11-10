"""Shin Kong Securities' monthly statement (證券月對帳單): a brokerage account's trades in the month and its
holdings at the end of it (ADR 0017).

Page 1 prints the month (以下是 115 年 07 月的綜合證券月對帳單) and the account's masked number beside the
owner's name. 交易資料明細 lists the month's trades, dated by month and day, then a 合計 row on two lines.
A buy paid in advance prints 0 to pay and, on the row below, the amount prepaid (預繳金). 庫存資料明細
lists the holdings, two securities to a row, as of the day it prints: a few days into the next month,
counting that day's trades, which the next month's statement lists. The columns between 交易稅 and
應收付金額, and every holding column but 普通, belong to margin trading, short selling, and pledged shares:
none has been seen, so a number in one raises a layout error rather than being read as something else.

The PDF's text writes some characters in compatibility forms (⼀ for 一, ⾦ for 金), so every word is
normalized before it is read; securities' names then read as the exchanges spell them.
"""

import re
from collections.abc import Sequence
from dataclasses import dataclass, field, replace
from datetime import date
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import Reconciliation, SecuritiesHolding, SecuritiesStatement, SecuritiesTrade, TradeSide
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.layout import nearest_column, rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import (
    MASKED_ACCOUNT,
    MONTH_DAY,
    ROC_DATE,
    amount,
    clean,
    day_in_period,
    find,
    month_end,
    roc_date,
    roc_year,
    token,
)

_MONTH = re.compile(r"以下是 ?(\d{3}) ?年 ?(\d{1,2}) ?月的綜合證券月對帳單")
_ACCOUNT = re.compile(rf"\( ?({MASKED_ACCOUNT}) ?\)", re.ASCII)
_MONTH_DAY = token(MONTH_DAY)
_ROC_DATE = token(ROC_DATE)
# Never negative: 應收付金額 says which way with (收) or (付).
_AMOUNT = token(r"\d[\d,]*(?:\.\d+)?")
_NET = re.compile(r"(\d[\d,]*)\((付|收)\)")
_TRADE_COLUMNS = ("成交股數", "單價", "價金", "手續費", "交易稅")
_HOLDING_COLUMNS = ("普通", "融資", "融券", "借貸擔保品")
# 預繳金 = 12,345 抵繳金額 = 0 留置款 = 0 借貸還款 = 0
_PREPAID = re.compile(r"預繳金 = ([\d,]+) 抵繳金額 = ([\d,]+) 留置款 = ([\d,]+) 借貸還款 = ([\d,]+)")


@dataclass
class _Totals:
    amount: Decimal
    fee: Decimal
    tax: Decimal
    net: Decimal


@dataclass
class _Reader:
    start: date
    end: date
    section: str = ""
    trade_columns: dict[str, Word] = field(default_factory=dict)
    # The x range of the margin and short-selling columns.
    margin: tuple[float, float] | None = None
    trades: list[SecuritiesTrade] = field(default_factory=list)
    totals: _Totals | None = None
    holdings_date: date | None = None
    # Each half of a holdings row: where it starts and its columns.
    halves: list[tuple[float, dict[str, Word]]] = field(default_factory=list)
    holdings: list[SecuritiesHolding] = field(default_factory=list)

    def read_page(self, words: Sequence[Word]) -> None:
        for row in rows_of(Word(clean(w.text), w.x0, w.x1, w.top) for w in words):
            texts = [w.text for w in row]
            line = " ".join(texts)
            if "交易資料明細" in texts:
                self.section = "trades"
            elif "的庫存資料明細" in texts and _ROC_DATE.fullmatch(texts[0]):
                self.section, self.holdings_date = "holdings", roc_date(texts[0])
            elif "其他說明及宣導事項" in texts:
                self.section = ""
            elif self.section == "trades":
                self._trades_row(row, texts, line)
            elif self.section == "holdings":
                self._holdings_row(row, texts)

    def _trades_row(self, row: list[Word], texts: list[str], line: str) -> None:
        if "融資自備款" in texts:
            self.margin = (min(w.x0 for w in row if w.text != "成交"), max(w.x1 for w in row))
        elif "成交股數" in texts:
            self.trade_columns = {w.text: w for w in row}
        elif _MONTH_DAY.fullmatch(texts[0]):
            self.trades.append(self._trade(row))
        elif texts[0] == "預繳金":
            prepaid = _PREPAID.fullmatch(line)
            if prepaid is None or not self.trades or any(amount(prepaid[i]) for i in (2, 3, 4)):
                raise LayoutError(f"{line}: a prepayment not seen yet")
            self.trades[-1] = replace(self.trades[-1], prepaid=amount(prepaid[1]))
        elif texts[0] == "合計":
            self.totals = self._totals(row)
        elif all(_AMOUNT.fullmatch(text) for text in texts) and (
            self.totals is None or any(amount(text) for text in texts)
        ):
            # The second line of a row holds margin and short selling's amounts, 0 for cash trades.
            raise LayoutError(f"{line}: margin or short-selling amounts, a layout not seen yet")

    def _columns(self, row: list[Word]) -> tuple[dict[str, Decimal], Decimal | None, list[Word]]:
        """A trade or 合計 row's amounts by column, its net, and the words left over."""
        missing = [name for name in (*_TRADE_COLUMNS, "類別", "應收付金額") if name not in self.trade_columns]
        if missing or self.margin is None:
            raise LayoutError(f"{row[0].text}: a row before its table's headings")
        columns = {name: self.trade_columns[name] for name in _TRADE_COLUMNS}
        found: dict[str, Decimal] = {}
        net: Decimal | None = None
        rest: list[Word] = []
        for word in row[1:]:
            if (paid := _NET.fullmatch(word.text)) is not None:
                net = amount(paid[1]) * (1 if paid[2] == "收" else -1)
            elif _AMOUNT.fullmatch(word.text) and self.margin[0] - 5 <= word.x0 and word.x1 <= self.margin[1] + 5:
                if amount(word.text):
                    raise LayoutError(f"{row[0].text}: margin or short-selling amounts, a layout not seen yet")
            elif _AMOUNT.fullmatch(word.text) and word.x0 > self.trade_columns["成交股數"].x0 - 30:
                column = nearest_column(word, columns)
                if column in found:
                    raise LayoutError(f"{row[0].text}: two amounts under {column}")
                found[column] = amount(word.text)
            else:
                rest.append(word)
        return found, net, rest

    def _trade(self, row: list[Word]) -> SecuritiesTrade:
        when = row[0].text
        found, net, rest = self._columns(row)
        heading = self.trade_columns["類別"]
        categories = [w for w in rest if w.x0 >= heading.x0 - 5 and w.x1 <= heading.x1 + 5]
        name = clean(" ".join(w.text for w in rest if w not in categories))
        if len(categories) != 1 or not name:
            raise LayoutError(f"{when}: no name or no single category")
        category = categories[0].text
        if "資" in category or "券" in category or category[-1] not in "買賣":
            raise LayoutError(f"{when} {category}: margin or short selling, a layout not seen yet")
        missing = [column for column in ("成交股數", "單價", "價金", "手續費") if column not in found]
        if missing or net is None:
            raise LayoutError(f"{when} {name}: no {', '.join(missing) or '應收付金額'}")
        return SecuritiesTrade(
            date=day_in_period(when, self.start, self.end),
            name=name,
            side=TradeSide.BUY if category.endswith("買") else TradeSide.SELL,
            shares=found["成交股數"],
            price=found["單價"],
            category=category,
            amount=found["價金"],
            fee=found["手續費"],
            tax=found.get("交易稅", Decimal(0)),
            net=net,
        )

    def _totals(self, row: list[Word]) -> _Totals:
        found, net, rest = self._columns(row)
        if rest or net is None or "成交股數" in found or "單價" in found:
            raise LayoutError(f"合計: {' '.join(w.text for w in row)} is not a row of totals")
        zero = Decimal(0)
        return _Totals(found.get("價金", zero), found.get("手續費", zero), found.get("交易稅", zero), net)

    def _holdings_row(self, row: list[Word], texts: list[str]) -> None:
        if "普通" in texts:
            right = next(w for w in row if w.text == "證券名稱")
            left = {w.text: w for w in row if w.x1 < right.x0 and w.text in _HOLDING_COLUMNS}
            right_columns = {w.text: w for w in row if w.x0 > right.x0 and w.text in _HOLDING_COLUMNS}
            self.halves = [(0.0, left), (right.x0 - 5, right_columns)]
            return
        if not self.halves:
            raise LayoutError(f"{texts[0]}: a holding before its table's headings")
        bounds = [start for start, _ in self.halves[1:]] + [float("inf")]
        for (start, columns), stop in zip(self.halves, bounds, strict=True):
            half = [w for w in row if start <= w.x0 < stop]
            if half:
                self.holdings.append(_holding(half, columns))


def _holding(words: list[Word], columns: dict[str, Word]) -> SecuritiesHolding:
    name = clean(" ".join(w.text for w in words if not _AMOUNT.fullmatch(w.text)))
    shares: dict[str, Decimal] = {}
    for word in words:
        if _AMOUNT.fullmatch(word.text):
            column = nearest_column(word, columns)
            if column in shares:
                raise LayoutError(f"{name}: two amounts under {column}")
            shares[column] = amount(word.text)
    if any(column != "普通" and value for column, value in shares.items()):
        raise LayoutError(f"{name}: margin, short, or pledged shares, a layout not seen yet")
    if not name or "普通" not in shares:
        raise LayoutError(f"{' '.join(w.text for w in words)}: not a name and its shares")
    return SecuritiesHolding(name, shares["普通"])


class SkisSecuritiesParser(StatementParser):
    name = "skis.securities"
    version = "1"
    institution = "skis"
    # 新光證券115/07證券月對帳單, from SKSEM@ebill.sk88.com.tw. Its daily reports (新光證券2026/07/24證券帳務回報通知)
    # come from the same domain and are not read (ADR 0017).
    sender_domains = ("sk88.com.tw",)
    subject_terms = ("證券月對帳單",)
    filename_pattern = re.compile(r"證券月對帳單\.pdf$")
    text_marker = "sk88.com.tw"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 電子帳單密碼 (您的身分證字號，英文字母需為大寫).
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        first = [clean(text) for text in document.page_texts(1)]
        month = find(_MONTH, first, "statement month")
        year, number = roc_year(int(month.group(1))), int(month.group(2))
        start, end = date(year, number, 1), month_end(year, number)
        account = find(_ACCOUNT, first, "account number").group(1)

        reader = _Reader(start, end)
        for page in range(document.page_count()):
            reader.read_page(document.page_words(page))
        if reader.holdings_date is None:
            raise LayoutError("no holdings section found")
        if reader.trades and reader.totals is None:
            raise LayoutError("trades without their 合計 row")

        statement = SecuritiesStatement(
            institution=self.institution,
            account_number=account,
            period_start=start,
            period_end=end,
            holdings_date=reader.holdings_date,
            holdings=reader.holdings,
            trades=reader.trades,
            reconciliation=_reconcile(reader.trades, reader.totals),
        )
        return Parsed(statement)


def _reconcile(trades: list[SecuritiesTrade], totals: _Totals | None) -> Reconciliation:
    """The trades against the 合計 row, column by column: the first that does not add up, else the net."""
    stated = totals or _Totals(Decimal(0), Decimal(0), Decimal(0), Decimal(0))
    checks = [
        ("價金", sum((t.amount for t in trades), Decimal(0)), stated.amount),
        ("手續費", sum((t.fee for t in trades), Decimal(0)), stated.fee),
        ("交易稅", sum((t.tax for t in trades), Decimal(0)), stated.tax),
    ]
    for label, computed, printed in checks:
        if computed != printed:
            return Reconciliation(computed, printed, label)
    return Reconciliation(sum((t.net for t in trades), Decimal(0)), stated.net, "應收付金額")
