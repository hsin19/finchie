import re
from decimal import Decimal

from finchie_parser.model import Kind, PointsExpiry, Reconciliation, Statement, Transaction
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
from finchie_parser.text import amount, clean, month_day, month_end, roc_date

_DUE = re.compile(r"^(\d{3}/\d\d/\d\d) \d+(?:\.\d+)?%", re.ASCII)
# The closing date is printed next to the cash-advance limit: "115/09/07 24,000 / 240,000 元".
_CLOSING = re.compile(r"^(\d{3}/\d\d/\d\d) [\d,]+ / [\d,]+ 元", re.ASCII)
_SUBTOTAL = re.compile(r"^本期合計： TWD (-?[\d,]+)$", re.ASCII)
_TOTAL_DUE = re.compile(r"^本期應繳總金額： TWD (-?[\d,]+)$", re.ASCII)
_CARD = re.compile(r"^卡號：\d{4}-X{4}-X{4}-(\d{4})", re.ASCII)
# Foreign lines add "<conversion date> <currency> <amount>" before the billed TWD amount.
# A trailing letter marks the mobile wallet (A = Apple Pay, G = Google Pay, ...).
_ROW = re.compile(
    r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?)"
    r"(?: \d\d/\d\d (?P<cur>[A-Z]{3}) (?P<orig>-?[\d,]+(?:\.\d+)?))?"
    r" TWD (?P<amt>-?[\d,]+)(?: [AGQS])?$",
    re.ASCII,
)
_PAYMENT = re.compile(r"^(?P<date>\d\d/\d\d) (?P<desc>\D.*) TWD (?P<amt>-[\d,]+)$", re.ASCII)
# 點數明細, its headings an image (ADR 0019): "<上期累積點數> 點 <本期加點> 點 <本期調整點數> 點 <本期扣點> 點
# <本期結餘點數> 點". The table names no program; its notes call the points e point.
_POINTS = re.compile(
    r"^(?P<opening>-?[\d,]+) 點 (?P<added>-?[\d,]+) 點 (?P<adjusted>-?[\d,]+) 點 (?P<used>-?[\d,]+) 點"
    r" (?P<closing>-?[\d,]+) 點$"
)
_POINTS_NAME = "e point"
# "※您有e point點數即將到期：2026/10共0點、2026/11共0點，…": each month's, by its last day.
_POINTS_EXPIRING = re.compile(r"(?P<y>\d{4})/(?P<m>\d{1,2})共(?P<n>[\d,]+)點")


class EsunParser(StatementParser):
    name = "esun.credit_card"
    version = "2"
    institution = "esun"
    # 玉山銀行2026年09月信用卡電子帳單; the combined (deposit) statement, 玉山銀行2026年09月綜合對帳單,
    # comes from the same address.
    sender_domains = ("esunbank.com",)
    subject_terms = ("信用卡電子帳單",)
    filename_pattern = re.compile(r"^ESUN_Estatement_\d+\.pdf$", re.IGNORECASE)
    text_marker = "玉山"

    def passwords(self, identity: Identity) -> list[str]:
        # Neither the e-mail nor the FAQ pages it links state a rule; it only says the password can be
        # changed or turned off in online banking. The owner's statements open without one.
        return []

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        closing = roc_date(find(_CLOSING, texts, "closing date").group(1))
        due = roc_date(find(_DUE, texts, "due date").group(1))
        subtotal = amount(find(_SUBTOTAL, texts, "本期合計").group(1))
        total_due = amount(find(_TOTAL_DUE, texts, "本期應繳總金額").group(1))

        transactions: list[Transaction] = []
        program: re.Match[str] | None = None
        expiries: list[PointsExpiry] = []
        section = ""
        card = ""
        for line in (line for text in texts for line in text.splitlines()):
            if match := _POINTS.match(line):
                program = match
            elif "即將到期" in line and _POINTS_NAME in line:
                expiries = [
                    PointsExpiry(amount(m["n"]), month_end(int(m["y"]), int(m["m"])))
                    for m in _POINTS_EXPIRING.finditer(line)
                ]
            elif line.startswith("本期費用明細"):
                section, card = "fees", ""
            elif line.startswith("本期消費明細"):
                section = "purchases"
            elif line.startswith("本期合計"):
                section = ""
            elif match := _CARD.match(line):
                card = match.group(1)
            elif match := _PAYMENT.match(line):
                transactions.append(
                    Transaction(
                        transaction_date=month_day(match["date"], closing),
                        description=clean(match["desc"]),
                        kind=Kind.PAYMENT,
                        amount=amount(match["amt"]),
                    )
                )
            elif section and (match := _ROW.match(line)):
                description = clean(match["desc"])
                value = amount(match["amt"])
                # Foreign merchants that bill in TWD print TWD as the original currency too.
                foreign = bool(match["cur"]) and match["cur"] != "TWD"
                transactions.append(
                    Transaction(
                        transaction_date=month_day(match["date"], closing),
                        posting_date=month_day(match["posted"], closing),
                        description=description,
                        kind=_kind(description, value),
                        amount=value,
                        original_amount=amount(match["orig"]) if foreign else None,
                        original_currency=match["cur"] if foreign else "",
                        card_last4=card if section == "purchases" else "",
                    )
                )

        warnings: list[str] = []
        points = []
        if program:
            points.append(
                points_program(
                    _POINTS_NAME,
                    opening=amount(program["opening"]),
                    lines=(
                        earned("本期加點", amount(program["added"])),
                        adjusted("本期調整點數", amount(program["adjusted"])),
                        used("本期扣點", amount(program["used"])),
                    ),
                    closing=amount(program["closing"]),
                    expiries=expiries,
                    warnings=warnings,
                )
            )

        statement = Statement(
            institution=self.institution,
            closing_date=closing,
            due_date=due,
            currency="TWD",
            total_due=total_due,
            transactions=transactions,
            reconciliation=Reconciliation(sum_new_charges(transactions), subtotal, "本期合計"),
            points=points,
        )
        return Parsed(statement, warnings)


def _kind(description: str, value: Decimal) -> Kind:
    if "國外交易服務費" in description:
        return Kind.FOREIGN_TRANSACTION_FEE
    if "年費" in description:
        return Kind.ANNUAL_FEE
    if "回饋" in description:
        return Kind.REBATE
    if value < 0:
        return Kind.REFUND
    return Kind.PURCHASE
