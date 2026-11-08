import re
from decimal import Decimal

from finchie_parser.model import Kind, Reconciliation, Statement, Transaction
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser, find, sum_new_charges
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


class TaishinParser(StatementParser):
    name = "taishin.credit_card"
    version = "3"
    institution = "taishin"
    # 台新信用卡電子帳單 2026年10月; the combined (deposit) statement, 台新銀行綜合對帳單 2026年9月份,
    # comes from the same address.
    sender_domains = ("taishinbank.com.tw",)
    subject_pattern = re.compile(r"信用卡電子帳單")
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

        statement = Statement(
            institution=self.institution,
            closing_date=closing,
            due_date=due,
            currency="TWD",
            total_due=total_due,
            transactions=transactions,
            reconciliation=Reconciliation(sum_new_charges(transactions), new_charges, "本期新增款項"),
        )
        return Parsed(statement)


def _kind(description: str, value: Decimal) -> Kind:
    if "國外交易服務費" in description:
        return Kind.FOREIGN_TRANSACTION_FEE
    if "年費" in description:
        return Kind.ANNUAL_FEE
    if value < 0 and ("扣繳" in description or "繳款" in description):
        return Kind.PAYMENT
    if "折抵" in description:
        return Kind.REBATE
    if value < 0:
        return Kind.REFUND
    return Kind.PURCHASE
