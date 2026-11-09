import re
from decimal import Decimal

from finchie_parser.model import Kind, PointsExpiry, PointsProgram, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import (
    Parsed,
    StatementParser,
    adjusted,
    earned,
    find,
    points_program,
    sum_new_charges,
    used,
)
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import amount, clean, roc_date

_CLOSING = re.compile(r"^帳單結帳日 (\d{3}/\d\d/\d\d)$", re.ASCII)
_DUE = re.compile(r"^繳款截止日 (\d{3}/\d\d/\d\d)$", re.ASCII)
_NEW_CHARGES = re.compile(r"^\+本期新增款項 (-?[\d,]+)$", re.ASCII)
_TOTAL_DUE = re.compile(r"^=本期累計應繳金額 (-?[\d,]+)$", re.ASCII)
_CARD = re.compile(r"\(卡號末四碼:(\d{4})\)", re.ASCII)
# "<date> <posted> <description> <amount>" then either "<MMDD> <country> <currency> <foreign amount>"
# for foreign-currency lines or just "<country>". Long descriptions wrap: the row then has an empty
# description, with its first half on the line above and the rest (usually the city) below.
# Refunds of foreign purchases print a negative foreign amount too. The amount must follow a space:
# otherwise a line the pattern does not fully fit can still match with the amount cut out of a
# decimal ("-27.03" read as 3), which is worse than not matching at all.
_ROW = re.compile(
    r"^(?P<date>\d{3}/\d\d/\d\d) (?P<posted>\d{3}/\d\d/\d\d) (?:(?P<desc>.*?) )?(?P<amt>-?[\d,]+)"
    r"(?: \d{4} (?P<fxcountry>[A-Z]{2}) (?P<cur>[A-Z]{3}) (?P<orig>-?[\d,]+(?:\.\d+)?)| (?P<country>[A-Z]{2}))?$",
    re.ASCII,
)
# 信用卡點數 (ADR 0019): a row per figure, a column per program, "**" where the card earns none of it:
# "上期結餘點數/里數 12,468 **". The first column is 台新Point(信用卡), 大集大利點數 until 2022-06.
_POINTS_ROW = re.compile(
    r"^(?P<label>上期結餘點數/里數|新增回饋|活動回饋/調整|本期使用點數/里數|本期結餘回饋)"
    r" (?P<first>\S+) (?P<second>\S+)$"
)
_POINTS_OLD_NAME = "大集大利點數"
_POINTS_NAME = "台新Point(信用卡)"
_MILES_NAME = "亞洲萬里通里數"
# "台新Point(信用卡)點數到期提醒：您有0點將於\n116/01/31到期", "【大集大利點數到期提醒】 您有0點將於…": the
# first column's. Only after its heading, so that a promotion worded alike elsewhere on the bill is not it.
_POINTS_EXPIRING = re.compile(r"到期提醒[：:】]?\s*您有(?P<n>[\d,]+)點將於\s*(?P<date>\d{3}/\d\d/\d\d)到期")


class TaishinParser(StatementParser):
    name = "taishin.credit_card"
    version = "6"
    institution = "taishin"
    # 台新信用卡電子帳單 2026年10月; the combined (deposit) statement, 台新銀行綜合對帳單 2026年9月份,
    # comes from the same address.
    sender_domains = ("taishinbank.com.tw",)
    subject_terms = ("信用卡電子帳單",)
    filename_pattern = re.compile(r"^TSB_Creditcard_Estatement_\d+\.pdf$", re.IGNORECASE)
    text_marker = "台新"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: since the 113/12 bill, 身分證字號後2碼 + 生日之月日4碼 (6 characters); bills up to
        # 113/11 open with the national ID, capital letter. The payment slip beside it takes the same.
        national_id = identity.require_national_id().upper()
        return [national_id[-2:] + identity.require_birth_date().strftime("%m%d"), national_id]

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        closing = roc_date(find(_CLOSING, texts, "帳單結帳日").group(1))
        due = roc_date(find(_DUE, texts, "繳款截止日").group(1))
        new_charges = amount(find(_NEW_CHARGES, texts, "本期新增款項").group(1))
        total_due = amount(find(_TOTAL_DUE, texts, "本期累計應繳金額").group(1))

        # (page index, line): a wrapped description never continues across a page break.
        lines = [(page, line) for page, text in enumerate(texts) for line in text.splitlines() if line.strip()]
        transactions: list[Transaction] = []
        card = ""
        i = 0
        while i < len(lines):
            page, line = lines[i]
            if match := _CARD.search(line):
                card = match.group(1)
            elif match := _ROW.match(line):
                description = match["desc"] or ""
                if not description:
                    above = lines[i - 1][1] if i > 0 and lines[i - 1][0] == page else ""
                    below = lines[i + 1][1] if i + 1 < len(lines) and lines[i + 1][0] == page else ""
                    description = f"{above} {below}"
                    if below:
                        i += 1
                value = amount(match["amt"])
                description = clean(description)
                kind = _kind(description, value)
                transactions.append(
                    Transaction(
                        transaction_date=roc_date(match["date"]),
                        posting_date=roc_date(match["posted"]),
                        description=description,
                        kind=kind,
                        amount=value,
                        original_amount=amount(match["orig"]) if match["orig"] else None,
                        original_currency=match["cur"] or "",
                        country=match["fxcountry"] or match["country"] or "",
                        card_last4="" if kind is Kind.PAYMENT else card,
                    )
                )
            i += 1

        warnings: list[str] = []
        statement = Statement(
            institution=self.institution,
            closing_date=closing,
            due_date=due,
            currency="TWD",
            total_due=total_due,
            transactions=transactions,
            reconciliation=Reconciliation(sum_new_charges(transactions), new_charges, "本期新增款項"),
            points=_points(texts, warnings),
        )
        return Parsed(statement, warnings)


def _kind(description: str, value: Decimal) -> Kind:
    if "國外交易服務費" in description:
        return Kind.FOREIGN_TRANSACTION_FEE
    if "年費" in description:
        return Kind.ANNUAL_FEE
    # Older bills word it 台新自動扣款, and before that 您的付款已收到,謝謝您!.
    if value < 0 and any(word in description for word in ("扣繳", "繳款", "自動扣款", "付款已收到")):
        return Kind.PAYMENT
    if "折抵" in description:
        return Kind.REBATE
    if value < 0:
        return Kind.REFUND
    return Kind.PURCHASE


def _points(texts: list[str], warnings: list[str]) -> list[PointsProgram]:
    """Each column of the points table with a figure in it, as a program of its own."""
    rows: dict[str, tuple[str, str]] = {}
    first_name = _POINTS_NAME
    for text in texts:
        lines = text.splitlines()
        for i, line in enumerate(lines):
            if match := _POINTS_ROW.match(line):
                rows[match["label"]] = (match["first"], match["second"])
                heading = lines[max(0, i - 3) : i]
                if match["label"] == "上期結餘點數/里數" and any(_POINTS_OLD_NAME in above for above in heading):
                    first_name = _POINTS_OLD_NAME
    if len(rows) < 5:
        return []
    expiring = _POINTS_EXPIRING.search("\n".join(texts))
    programs = []
    for column, name in enumerate((first_name, _MILES_NAME)):
        figures = {label: values[column] for label, values in rows.items()}
        if any(value == "**" for value in figures.values()):
            continue
        expiries = []
        if column == 0 and expiring:
            expiries.append(PointsExpiry(amount(expiring["n"]), roc_date(expiring["date"])))
        programs.append(
            points_program(
                name,
                opening=amount(figures["上期結餘點數/里數"]),
                lines=(
                    earned("新增回饋", amount(figures["新增回饋"])),
                    adjusted("活動回饋/調整", amount(figures["活動回饋/調整"])),
                    used("本期使用點數/里數", amount(figures["本期使用點數/里數"])),
                ),
                closing=amount(figures["本期結餘回饋"]),
                expiries=expiries,
                warnings=warnings,
            )
        )
    return programs
