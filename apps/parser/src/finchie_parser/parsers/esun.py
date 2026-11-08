import re
from decimal import Decimal

from finchie_parser.model import Kind, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser, find, sum_new_charges
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import amount, clean, month_day, roc_date

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


class EsunParser(StatementParser):
    name = "esun.credit_card"
    version = "1"
    institution = "esun"
    # 玉山銀行2026年09月信用卡電子帳單; the combined (deposit) statement, 玉山銀行2026年09月綜合對帳單,
    # comes from the same address.
    sender_domains = ("esunbank.com",)
    subject_pattern = re.compile(r"信用卡電子帳單")
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
        section = ""
        card = ""
        for line in (line for text in texts for line in text.splitlines()):
            if line.startswith("本期費用明細"):
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

        statement = Statement(
            institution=self.institution,
            closing_date=closing,
            due_date=due,
            currency="TWD",
            total_due=total_due,
            transactions=transactions,
            reconciliation=Reconciliation(sum_new_charges(transactions), subtotal, "本期合計"),
        )
        return Parsed(statement)


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
