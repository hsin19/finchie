import re
from datetime import date
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import Kind, PointsExpiry, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import (
    Parsed,
    StatementParser,
    earned,
    find,
    points_program,
    sum_new_charges,
    used,
)
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import amount, clean, month_day, month_end, roc_date

# 信用卡回饋總覽, its headings unmapped glyphs (ADR 0019): "<program> <上期累計> <本期新增/扣除> <本期贈送>
# <本期兌/轉換> <本期結餘>", e.g. "小樹點(信用卡) 3,190 64 0 1 3,253".
_POINTS = re.compile(
    r"^(?P<name>\S*(?:點|里數)\S*) (?P<opening>-?[\d,]+) (?P<added>-?[\d,]+) (?P<gift>-?[\d,]+)"
    r" (?P<used>-?[\d,]+) (?P<closing>-?[\d,]+)$"
)
# "您有88點小樹點(信用卡)於本月到期；共計有669點小樹點(信用卡)將於116年3月31日前陸續到期": the closing month's, and
# the next twelve months' together, the closing month's included.
_POINTS_EXPIRING = re.compile(
    r"您有(?P<month>[\d,]+)點(?P<name>\S+?)於本月到期；共計有(?P<total>[\d,]+)點\S+?"
    r"將於(?P<y>\d{3})年(?P<m>\d{1,2})月(?P<d>\d{1,2})日前陸續到期"
)
# Page 1 prints the closing date next to the credit limit: "115/10/01 140,000".
_CLOSING = re.compile(r"^(\d{3}/\d\d/\d\d) [\d,]+$", re.ASCII)
# The auto-debit notice carries the due date, already moved past holidays.
_DUE = re.compile(r"將於\s*(\d{3}/\d\d/\d\d)", re.ASCII)
_NEW_CHARGES = re.compile(r"^(?:正卡|附卡)本期消費 (-?[\d,]+)$", re.ASCII)
_TOTAL_DUE = re.compile(r"^本期應繳總額 (-?[\d,]+)$", re.ASCII)
_PAYMENTS = re.compile(r"^繳款小計 (-?[\d,]+)$", re.ASCII)
# "<date> <posted> <description> <amount> <card last 4> [<wallet card last 4>] <country> <currency>
#  [<foreign amount> [<conversion date>]]"; a refund's foreign amount is negative too.
_ROW = re.compile(
    r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>-?[\d,]+) (?P<card>\d{4})(?: \d{4})?"
    r" (?P<country>[A-Z]{2}) (?P<cur>[A-Z]{3})(?: (?P<orig>-?[\d,]+(?:\.\d+)?)(?: \d\d/\d\d)?)?$",
    re.ASCII,
)
# Fees and the annual fee name no country or currency:
# "<date> <posted> <description> <amount> <card last 4>", e.g. "12/21 01/01 國外交易手續費 -AUSTRA 6 7218".
_PLAIN_ROW = re.compile(
    r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>-?[\d,]+) (?P<card>\d{4})$", re.ASCII
)
# "<date> <posted> <description> <negative amount>": no card. The description can hold digits ("ATM轉帳 822").
_PAYMENT = re.compile(r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>-[\d,]+)$", re.ASCII)


class CathayParser(StatementParser):
    name = "cathay.credit_card"
    version = "5"
    institution = "cathay"
    sender_domains = ("cathaybk.com.tw",)
    subject_terms = ("信用卡", "電子帳單")
    filename_pattern = re.compile(r"^信用卡電子帳單消費明細_\d+\.pdf$")
    text_marker = "國泰世華"

    def passwords(self, identity: Identity) -> list[str]:
        # The e-mail: 電子帳單密碼 (您的身分證字號，英文字母大寫).
        return [identity.require_national_id().upper()]

    def matches_text(self, document: Document) -> bool:
        # The bank's name sits in an image on page 1; the fine print on page 2 names it.
        return any(self.text_marker in text for text in document.page_texts(2))

    def parse(self, document: Document, ocr: Ocr | None) -> Parsed:
        texts = document.page_texts()
        closing = roc_date(find(_CLOSING, texts[:1], "closing date").group(1))
        due = roc_date(find(_DUE, texts, "due date").group(1))
        total_due = amount(find(_TOTAL_DUE, texts, "本期應繳總額").group(1))
        # One subtotal per card holder (正卡, 附卡). None until one is read: taking a missing subtotal as
        # zero would let a layout change that also hides every row reconcile as balanced and empty.
        new_charges: Decimal | None = None
        payments: Decimal | None = None

        transactions: list[Transaction] = []
        programs: list[re.Match[str]] = []
        expiring: dict[str, list[PointsExpiry]] = {}
        for line in (line for text in texts for line in text.splitlines()):
            if match := _NEW_CHARGES.match(line):
                new_charges = (new_charges or Decimal(0)) + amount(match.group(1))
            elif match := _PAYMENTS.match(line):
                payments = (payments or Decimal(0)) + amount(match.group(1))
            elif match := _ROW.match(line):
                value = amount(match["amt"])
                foreign = match["cur"] != "TWD"
                transactions.append(
                    Transaction(
                        transaction_date=month_day(match["date"], closing),
                        posting_date=month_day(match["posted"], closing),
                        description=clean(match["desc"]),
                        kind=Kind.REFUND if value < 0 else Kind.PURCHASE,
                        amount=value,
                        original_amount=amount(match["orig"]) if foreign and match["orig"] else None,
                        original_currency=match["cur"] if foreign else "",
                        country=match["country"],
                        card_last4=match["card"],
                    )
                )
            elif match := _PLAIN_ROW.match(line):
                value = amount(match["amt"])
                description = clean(match["desc"])
                transactions.append(
                    Transaction(
                        transaction_date=month_day(match["date"], closing),
                        posting_date=month_day(match["posted"], closing),
                        description=description,
                        kind=_plain_kind(description, value),
                        amount=value,
                        card_last4=match["card"],
                    )
                )
            elif match := _PAYMENT.match(line):
                transactions.append(
                    Transaction(
                        transaction_date=month_day(match["date"], closing),
                        posting_date=month_day(match["posted"], closing),
                        description=clean(match["desc"]),
                        kind=Kind.PAYMENT,
                        amount=amount(match["amt"]),
                    )
                )
            elif match := _POINTS.match(line):
                programs.append(match)
            elif match := _POINTS_EXPIRING.search(line):
                by = date(int(match["y"]) + 1911, int(match["m"]), int(match["d"]))
                expiring[match["name"]] = [
                    PointsExpiry(amount(match["month"]), month_end(closing.year, closing.month)),
                    PointsExpiry(amount(match["total"]), by),
                ]

        if new_charges is None:
            raise LayoutError("statement field not found: 本期消費")
        warnings: list[str] = []
        paid = sum((tx.amount for tx in transactions if tx.kind is Kind.PAYMENT), Decimal(0))
        # Payments are not part of the new charges, so only this subtotal shows a payment line was missed.
        if payments is not None and paid != payments:
            warnings.append(f"payment lines add up to {paid}, but 繳款小計 is {payments}")

        points = [
            points_program(
                m["name"],
                opening=amount(m["opening"]),
                lines=(
                    earned("本期新增/扣除", amount(m["added"])),
                    earned("本期贈送", amount(m["gift"])),
                    used("本期兌/轉換", amount(m["used"])),
                ),
                closing=amount(m["closing"]),
                expiries=expiring.get(m["name"], ()),
                warnings=warnings,
            )
            for m in programs
        ]

        statement = Statement(
            institution=self.institution,
            closing_date=closing,
            due_date=due,
            currency="TWD",
            total_due=total_due,
            transactions=transactions,
            reconciliation=Reconciliation(sum_new_charges(transactions), new_charges, "本期消費"),
            points=points,
        )
        return Parsed(statement, warnings)


def _plain_kind(description: str, value: Decimal) -> Kind:
    # "國外退貨手續費": the fee on a refunded purchase, given back.
    if "國外交易手續費" in description or "國外退貨手續費" in description:
        return Kind.FOREIGN_TRANSACTION_FEE
    if "年費" in description:
        return Kind.ANNUAL_FEE
    return Kind.REFUND if value < 0 else Kind.PURCHASE
