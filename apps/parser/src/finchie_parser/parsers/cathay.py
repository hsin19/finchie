import re
from datetime import date
from decimal import Decimal

from finchie_parser.errors import LayoutError
from finchie_parser.model import Kind, PointsExpiry, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.kinds import card_kind
from finchie_parser.parsers.points import earned, points_program, used
from finchie_parser.parsers.reconcile import sum_new_charges
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import (
    AMOUNT,
    INTEGER,
    ROC_DATE,
    amount,
    clean,
    find,
    month_day,
    month_end,
    roc_date,
)

# 信用卡回饋總覽, its headings unmapped glyphs (ADR 0019): "<program> <上期累計> <本期新增/扣除> <本期贈送>
# <本期兌/轉換> <本期結餘>", e.g. "小樹點(信用卡) 3,190 64 0 1 3,253".
_POINTS = re.compile(
    rf"^(?P<name>\S*(?:點|里數)\S*) (?P<opening>{INTEGER}) (?P<added>{INTEGER}) (?P<gift>{INTEGER})"
    rf" (?P<used>{INTEGER}) (?P<closing>{INTEGER})$"
)
# "您有88點小樹點(信用卡)於本月到期；共計有669點小樹點(信用卡)將於116年3月31日前陸續到期": the closing month's, and
# the next twelve months' together, the closing month's included.
_POINTS_EXPIRING = re.compile(
    r"您有(?P<month>[\d,]+)點(?P<name>\S+?)於本月到期；共計有(?P<total>[\d,]+)點\S+?"
    r"將於(?P<y>\d{3})年(?P<m>\d{1,2})月(?P<d>\d{1,2})日前陸續到期"
)
# Page 1 prints the closing date next to the credit limit: "115/10/01 140,000".
_CLOSING = re.compile(rf"^({ROC_DATE}) [\d,]+$", re.ASCII)
# The auto-debit notice carries the due date, already moved past holidays.
_DUE = re.compile(rf"將於\s*({ROC_DATE})", re.ASCII)
_NEW_CHARGES = re.compile(rf"^(?:正卡|附卡)本期消費 ({INTEGER})$", re.ASCII)
_TOTAL_DUE = re.compile(rf"^本期應繳總額 ({INTEGER})$", re.ASCII)
_PAYMENTS = re.compile(rf"^繳款小計 ({INTEGER})$", re.ASCII)
# "<date> <posted> <description> <amount> <card last 4> [<wallet card last 4>] <country> <currency>
#  [<foreign amount> [<conversion date>]]"; a refund's foreign amount is negative too.
_ROW = re.compile(
    rf"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>{INTEGER}) (?P<card>\d{{4}})(?: \d{{4}})?"
    rf" (?P<country>[A-Z]{{2}}) (?P<cur>[A-Z]{{3}})(?: (?P<orig>{AMOUNT})(?: \d\d/\d\d)?)?$",
    re.ASCII,
)
# Fees and the annual fee name no country or currency:
# "<date> <posted> <description> <amount> <card last 4>", e.g. "12/21 01/01 國外交易手續費 -AUSTRA 6 7218".
_PLAIN_ROW = re.compile(
    rf"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>{INTEGER}) (?P<card>\d{{4}})$", re.ASCII
)
# "<date> <posted> <description> <negative amount>": no card. The description can hold digits ("ATM轉帳 822").
_PAYMENT = re.compile(r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?) (?P<amt>-\d[\d,]*)$", re.ASCII)


class CathayParser(StatementParser):
    name = "cathay.credit_card"
    version = "6"
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
                foreign = match["cur"] != "TWD"
                transactions.append(
                    _transaction(
                        match,
                        closing,
                        original_amount=amount(match["orig"]) if foreign and match["orig"] else None,
                        original_currency=match["cur"] if foreign else "",
                        country=match["country"],
                        card_last4=match["card"],
                    )
                )
            elif match := _PLAIN_ROW.match(line):
                transactions.append(_transaction(match, closing, card_last4=match["card"]))
            elif match := _PAYMENT.match(line):
                # No card: a payment, whatever its words.
                transactions.append(_transaction(match, closing, kind=Kind.PAYMENT))
            elif match := _POINTS.match(line):
                programs.append(match)
            elif match := _POINTS_EXPIRING.search(line):
                by = roc_date(f"{match['y']}/{match['m']}/{match['d']}")
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


def _transaction(
    match: re.Match[str],
    closing: date,
    *,
    kind: Kind | None = None,
    original_amount: Decimal | None = None,
    original_currency: str = "",
    country: str = "",
    card_last4: str = "",
) -> Transaction:
    value = amount(match["amt"])
    description = clean(match["desc"])
    return Transaction(
        transaction_date=month_day(match["date"], closing),
        posting_date=month_day(match["posted"], closing),
        description=description,
        kind=kind or card_kind(description, value),
        amount=value,
        original_amount=original_amount,
        original_currency=original_currency,
        country=country,
        card_last4=card_last4,
    )
