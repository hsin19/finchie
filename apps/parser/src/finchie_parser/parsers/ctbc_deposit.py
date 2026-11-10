"""CTBC's bank statement (對帳單): its deposit accounts and their lines (ADR 0013).

The PDF's Chinese text has no Unicode mapping, so only its digits, dates, and account numbers can be
read from the text layer; everything in Chinese (section titles, account types, currencies, descriptions,
the 上期結餘 and 本期結餘 rows) is read with OCR and matched to text-layer rows by height. Amounts always
come from the text layer.

Page 1 prints the period, which runs from mid-month to mid-month. Page 2 on lists the NT$ and foreign
currency holdings (資產持有明細), then each NT$ account's lines (交易明細): the balance brought forward,
the lines, a count of them, and the closing balance with the period's totals.
"""

import itertools
import re
from collections.abc import Sequence

from finchie_parser.errors import ConfigurationError, LayoutError
from finchie_parser.ocr import Ocr, OcrLine
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.deposit import Account, Accounts, Entry, amounts_by_column, moved_in
from finchie_parser.parsers.kinds import deposit_kind
from finchie_parser.parsers.layout import rows_of
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document, Word
from finchie_parser.text import AMOUNT, ISO_DATE, MASKED_ACCOUNT, amount, iso_date, token

_PERIOD = re.compile(rf"^({ISO_DATE}) ({ISO_DATE})$", re.ASCII | re.MULTILINE)
# CTBC masks the last digit too: 11111**2222*.
_ACCOUNT = token(rf"{MASKED_ACCOUNT}\*")
_DATE = token(ISO_DATE)
_AMOUNT = token(AMOUNT)
# A text-layer word's top sits about this far below the middle of the OCR box of the same row.
_ROW_OFFSET = 3.0
_ROW_TOLERANCE = 5.0
# The currencies CTBC names in Chinese, as far as its statements have shown them; another one fails
# by name rather than be guessed.
_CURRENCIES = {"美元": "USD", "日圓": "JPY", "日幣": "JPY", "歐元": "EUR", "人民幣": "CNY", "港幣": "HKD"}


class CtbcDepositParser(StatementParser):
    name = "ctbc.deposit"
    version = "4"
    institution = "ctbc"
    # 中國信託銀行電子對帳單 11510, from ebill@estats.ctbcbank.com.
    sender_domains = ("ctbcbank.com",)
    subject_terms = ("電子對帳單",)
    filename_pattern = re.compile(r"^CTBC_Bank_Estatement_\d+\.pdf$", re.IGNORECASE)
    # The only words of page 1 the text layer can spell: the bank's address.
    text_marker = "ctbcbank.com"
    needs_ocr = True

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 本國籍自然人客戶，請輸入【身分證字號(英文字母大寫)】. It can be changed or turned off in
        # online banking; one chosen there goes in PARSER_PDF_PASSWORDS__CTBC.
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        if ocr is None:
            raise ConfigurationError(
                "CTBC's statements need OCR (PARSER_OCR): their Chinese text cannot be read otherwise"
            )
        texts = document.page_texts()
        period = _PERIOD.search(texts[0])
        if period is None:
            raise LayoutError("statement period not found")
        reader = _Reader()
        for page in range(1, len(texts)):
            reader.read_page(document.page_words(page), ocr.read_column(document, page, 0, 10_000))
        start, end = iso_date(period.group(1)), iso_date(period.group(2))
        return Parsed(reader.accounts.statement(self.institution, start, end, "本期結餘"))


class _Reader:
    def __init__(self) -> None:
        self.accounts = Accounts()
        self.section = ""
        self.current: Account | None = None

    def read_page(self, words: Sequence[Word], ocr: Sequence[OcrLine]) -> None:
        labels = sorted(ocr, key=lambda line: line.top)
        # OCR rows and text-layer rows, interleaved top to bottom: titles and headings only OCR can read
        # decide what the text-layer rows below them are.
        events: list[tuple[float, OcrLine | list[Word]]] = [((line.top + line.bottom) / 2, line) for line in labels]
        events += [(row[0].top - _ROW_OFFSET, row) for row in rows_of(words)]
        headings: dict[str, OcrLine] = {}
        for _, event in sorted(events, key=lambda pair: pair[0]):
            if isinstance(event, OcrLine):
                text = event.text
                if text in ("台幣存款帳戶", "外幣存款帳戶") or text.endswith("交易明細"):
                    self.section, self.current, headings = text, None, {}
                elif text.startswith("優惠權益"):
                    self.section, self.current = "", None
                elif text in ("帳戶種類", "幣別", "摘要", "支出", "存入", "帳戶餘額"):
                    headings[text] = event
                continue
            row, beside = event, _beside(event, labels)
            if self.section.endswith("存款帳戶") and any(_ACCOUNT.fullmatch(w.text) for w in row):
                self._holding(row, beside, headings)
            elif self.section.endswith("交易明細") and row:
                self._lines_row(row, beside, headings)

    def _holding(self, row: list[Word], beside: list[OcrLine], headings: dict[str, OcrLine]) -> None:
        at = next(w for w in row if _ACCOUNT.fullmatch(w.text))
        number = at.text
        # Right of the number: an account kind's name left of it may carry digits of its own (MyWay數2,
        # whose 2 the text layer holds).
        amounts = [w for w in row if w.x0 > at.x1 and _AMOUNT.fullmatch(w.text)]
        if len(amounts) != 1:
            raise LayoutError(f"{number}: expected one balance, found {[w.text for w in amounts]}")
        name = " ".join(line.text for line in beside if line.left < at.x0 - 20)
        currency = "TWD"
        if self.section == "外幣存款帳戶":
            # By name, not by column: OCR does not always read the 幣別 heading apart from its neighbour.
            names = [line.text for line in beside if line.text in _CURRENCIES]
            if len(names) != 1:
                raise LayoutError(f"{number}: no known currency beside it in {[line.text for line in beside]}")
            currency = _CURRENCIES[names[0]]
        self.accounts.add(number, currency, name, amount(amounts[0].text))

    def _lines_row(self, row: list[Word], beside: list[OcrLine], headings: dict[str, OcrLine]) -> None:
        if not self.section.startswith("台幣"):
            raise LayoutError(f"{self.section}: foreign currency lines, a layout not seen yet")
        said = " ".join(line.text for line in beside)
        if _ACCOUNT.fullmatch(row[0].text):
            # The balance brought forward opens an account's lines, its number on that row only.
            found = self.accounts.get(row[0].text, "TWD")
            if found is None or "上期結餘" not in said:
                raise LayoutError(f"{row[0].text}: lines for an account the holdings do not list")
            self.current = found
            found.opening = amount(row[-1].text)
            return
        if self.current is None or "本次合計" in said:
            return
        columns: dict[str, OcrLine] = {
            name: headings[name] for name in ("支出", "存入", "帳戶餘額") if name in headings
        }
        if len(columns) != 3:
            raise LayoutError("a line before its table's headings")
        by_column, _ = amounts_by_column(
            row,
            # Right-aligned under headings OCR places by their left edge: the column whose span holds it.
            lambda word: (
                _column(word, columns) if _AMOUNT.fullmatch(word.text) and not _DATE.fullmatch(word.text) else None
            ),
            said,
        )
        if "本期結餘" in said:
            stated = by_column.get("帳戶餘額")
            if stated is not None and stated != self.current.closing:
                raise LayoutError(f"{self.current.number}: 本期結餘 {stated} differs from its holding")
            self.current = None
            return
        if not _DATE.fullmatch(row[0].text) or "帳戶餘額" not in by_column:
            raise LayoutError(f"a row in the lines that is not one: {[w.text for w in row]} {said}")
        description_column = headings.get("摘要")
        description = " ".join(
            line.text for line in beside if description_column is None or abs(line.left - description_column.left) <= 40
        )
        self.current.lines.append(
            Entry(
                date=iso_date(row[0].text),
                description=description,
                amount=moved_in(by_column, "存入", "支出"),
                kind=deposit_kind(description),
                balance=by_column["帳戶餘額"],
            )
        )


def _beside(row: list[Word], labels: Sequence[OcrLine]) -> list[OcrLine]:
    """The OCR lines on the same printed row as a text-layer row, left to right."""
    middle = row[0].top - _ROW_OFFSET
    near = [line for line in labels if abs((line.top + line.bottom) / 2 - middle) <= _ROW_TOLERANCE]
    return sorted(near, key=lambda line: line.left)


def _column(word: Word, columns: dict[str, OcrLine]) -> str:
    """Columns run from one heading's left edge to the next's: an amount ends inside the one it is under."""
    order = sorted(columns, key=lambda name: columns[name].left)
    for name, following in itertools.pairwise(order):
        if word.x1 < columns[following].left:
            return name
    return order[-1]
