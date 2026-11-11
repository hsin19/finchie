import re
from dataclasses import dataclass
from datetime import date
from decimal import Decimal

import pycountry

from finchie_parser.errors import LayoutError
from finchie_parser.model import Expiry, Line, LineKind
from finchie_parser.ocr import Ocr, OcrLine
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.card import card_bill, card_line, check
from finchie_parser.parsers.kinds import by_sign, card_kind, card_kind_by_words, is_charge
from finchie_parser.parsers.points import Program, earned, points_program, used
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import AMOUNT, CURRENCY, INTEGER, ISO_DATE, amount, iso_date, month_day, next_month_end, token

# HSBC renders every label and every transaction description as an image; the text layer only
# carries dates and numbers. Page 1 lists the due date first, then the closing date.
_P1_DATE = re.compile(rf"^({ISO_DATE}) ", re.ASCII)
_FX_ROW = re.compile(
    rf"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<country>{CURRENCY}) (?P<cur>{CURRENCY})"
    rf" (?P<orig>{AMOUNT}) \d\d/\d\d (?P<amt>{INTEGER})$",
    re.ASCII,
)
# Fees, payments, and the annual fee: no country or currency.
_PLAIN_ROW = re.compile(rf"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<amt>{INTEGER})$", re.ASCII)
# An amount alone on a line or in a box: the statement's unlabelled figures.
_FIGURE = token(INTEGER)

# Unlabelled amounts on the transaction pages, in order:
#   1 previous balance, [payment and adjustment rows], 2 payments subtotal, 3 carried balance,
#   [new charge rows], 4 new charges subtotal, 5 total due.
_CARRIED_BALANCE, _NEW_CHARGES, _TOTAL_DUE = 3, 4, 5

# Description column, in PDF points.
_DESCRIPTION_LEFT, _DESCRIPTION_RIGHT = 120.0, 335.0

# 滙豐旅遊積分, the box right of page 1's summary (ADR 0019). Its labels are images like the rest, so its
# figures are read by position, top to bottom: 上月結餘積分, 本期新增積分, 本期使用積分(含調整), 本期積分總結餘,
# 次月到期點數. In PDF points: right of the summary's figures, above the notice under it.
_POINTS_NAME = "滙豐旅遊積分"
_POINTS_LEFT, _POINTS_BOTTOM = 450.0, 330.0


@dataclass
class _Row:
    match: re.Match[str]
    page_index: int
    top: float
    bottom: float
    new_charge: bool
    description: str = ""


class HsbcParser(StatementParser):
    name = "hsbc.credit_card"
    version = "5"
    institution = "hsbc"
    # 115年10月滙豐旅人無限卡信用卡帳單: the card's name is in the subject.
    sender_domains = ("hsbc.com.tw",)
    subject_terms = ("信用卡帳單",)
    filename_pattern = re.compile(r"^eStatement_\d{6}\.pdf$", re.IGNORECASE)
    text_marker = "HSBC"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 請輸入您的「身分證字號」開啟 …（英文字母需大寫）.
        return [identity.require_national_id().upper()]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        page1_dates = [match.group(1) for line in texts[0].splitlines() if (match := _P1_DATE.match(line))]
        if len(page1_dates) < 2:
            raise LayoutError("due and closing dates not found on page 1")
        due, closing = iso_date(page1_dates[0]), iso_date(page1_dates[1])

        rows, numbers = self._scan(document, document.page_count())
        if len(numbers) < _TOTAL_DUE:
            raise LayoutError("new charges subtotal and total due not found")

        warnings: list[str] = []
        if ocr is None:
            warnings.append("OCR disabled: HSBC transaction descriptions are empty")
        else:
            warnings += _attach_descriptions(document, rows, ocr)

        lines = [self._line(row, closing) for row in rows]
        computed = sum((line.amount for line, row in zip(lines, rows, strict=True) if row.new_charge), Decimal(0))
        statement = card_bill(
            self.institution,
            closing=closing,
            due=due,
            total_due=numbers[_TOTAL_DUE - 1],
            lines=lines,
            checks=[check("消費/新增款項/循環息/其他費用小計", computed, numbers[_NEW_CHARGES - 1])],
            programs=_points(document, closing, warnings),
        )
        return Parsed(statement, warnings)

    @staticmethod
    def _scan(document: Document, page_count: int) -> tuple[list[_Row], list[Decimal]]:
        rows: list[_Row] = []
        numbers: list[Decimal] = []
        for page_index in range(1, page_count):
            for line in document.page_lines(page_index):
                if len(numbers) >= _TOTAL_DUE:
                    return rows, numbers
                if _FIGURE.fullmatch(line.text):
                    numbers.append(amount(line.text))
                elif match := _FX_ROW.match(line.text) or _PLAIN_ROW.match(line.text):
                    new_charge = len(numbers) >= _CARRIED_BALANCE
                    rows.append(_Row(match, page_index, line.top, line.bottom, new_charge))
        return rows, numbers

    @staticmethod
    def _line(row: _Row, closing: date) -> Line:
        match = row.match
        value = amount(match["amt"])
        foreign = "cur" in match.re.groupindex
        description = row.description
        kind = card_kind(description, value) if foreign else _plain_kind(description, value, row.new_charge)
        currency = match["cur"] if foreign else ""
        return card_line(
            month_day(match["date"], closing),
            description,
            kind,
            value,
            posting_date=month_day(match["posted"], closing),
            original_amount=amount(match["orig"]) if foreign and currency != "TWD" else None,
            original_currency=currency if currency != "TWD" else "",
            country=_alpha2(match["country"]) if foreign else "",
        )


def _plain_kind(description: str, value: Decimal, new_charge: bool) -> LineKind:
    # Words before sections: the payments section also lists credits that are not payments, such as the
    # annual fee's waiver.
    if kind := card_kind_by_words(description, value):
        return kind
    if is_charge(description):
        return by_sign(value)
    # No such word (OCR off or too noisy): what nearly every plain row is, a payment or a foreign transaction fee.
    if not new_charge and value < 0:
        return LineKind.PAYMENT
    return LineKind.FOREIGN_TRANSACTION_FEE


def _attach_descriptions(document: Document, rows: list[_Row], ocr: Ocr) -> list[str]:
    warnings = []
    for page_index in sorted({row.page_index for row in rows}):
        lines = ocr.read_column(document, page_index, _DESCRIPTION_LEFT, _DESCRIPTION_RIGHT)
        for row in (row for row in rows if row.page_index == page_index):
            row.description = _text_on_baseline(lines, row.bottom)
            if not row.description:
                warnings.append(f"no OCR text for the row at page {page_index + 1}, y={row.bottom:.0f}")
    return warnings


def _text_on_baseline(lines: list[OcrLine], baseline: float) -> str:
    # pdfplumber reports the text-layer row as a ~1 pt band at its baseline, while an OCR box spans
    # the full glyph height above it. Their bottoms agree within about a point; rows are 12 pt apart.
    hits = [line for line in lines if abs(line.bottom - baseline) <= 3.0]
    return " ".join(line.text for line in sorted(hits, key=lambda line: line.left))


def _alpha2(alpha3: str) -> str:
    country = pycountry.countries.get(alpha_3=alpha3)
    return country.alpha_2 if country else ""


def _points(document: Document, closing: date, warnings: list[str]) -> list[Program]:
    """The travel points box, where the card has one; next month's expiring points lapse by its last day."""
    figures = [
        word
        for word in sorted(document.page_words(0), key=lambda w: w.top)
        if word.x0 >= _POINTS_LEFT and word.top < _POINTS_BOTTOM and _FIGURE.fullmatch(word.text)
    ]
    if not figures:
        return []
    if len(figures) != 5:
        warnings.append(f"{_POINTS_NAME}: {len(figures)} figures in its box, not 5; points not read")
        return []
    opening, added, spent, balance, lapsing = (amount(word.text) for word in figures)
    return [
        points_program(
            _POINTS_NAME,
            opening=opening,
            moves=(earned("本期新增積分", added), used("本期使用積分(含調整)", spent)),
            closing=balance,
            expiries=[Expiry(_POINTS_NAME, lapsing, next_month_end(closing))],
        )
    ]
