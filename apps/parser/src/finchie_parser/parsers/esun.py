import re

from finchie_parser.model import Expiry, Line, LineKind
from finchie_parser.ocr import Ocr
from finchie_parser.parsers.base import Parsed, StatementParser
from finchie_parser.parsers.card import card_bill, card_line, check, new_charges
from finchie_parser.parsers.kinds import card_kind
from finchie_parser.parsers.points import adjusted, earned, lapse_months_after_closing, points_program, used
from finchie_parser.passwords import Identity
from finchie_parser.pdf import Document
from finchie_parser.text import AMOUNT, CURRENCY, INTEGER, amount, clean, either_date, find, month_day, month_end

# Minguo dates ("115/09/07") up to the September 2026 bill, Gregorian ones ("2026/11/07") from October.
_DUE = re.compile(r"^(\d{3,4}/\d\d/\d\d) \d+(?:\.\d+)?%", re.ASCII)
# The closing date is printed next to the cash-advance limit: "115/09/07 24,000 / 240,000 元".
_CLOSING = re.compile(r"^(\d{3,4}/\d\d/\d\d) [\d,]+ / [\d,]+ 元", re.ASCII)
# 本期回饋／費用明細 from the October 2026 bill.
_FEES = re.compile(r"^本期(?:回饋／)?費用明細")
_SUBTOTAL = re.compile(rf"^本期合計： TWD ({INTEGER})$", re.ASCII)
_TOTAL_DUE = re.compile(rf"^本期應繳總金額： TWD ({INTEGER})$", re.ASCII)
_CARD = re.compile(r"^卡號：\d{4}-X{4}-X{4}-(\d{4})", re.ASCII)
# Foreign lines add "<conversion date> <currency> <amount>" before the billed TWD amount.
# A trailing letter marks the mobile wallet (A = Apple Pay, G = Google Pay, ...).
_ROW = re.compile(
    r"^(?P<date>\d\d/\d\d) (?P<posted>\d\d/\d\d) (?P<desc>.+?)"
    rf"(?: \d\d/\d\d (?P<cur>{CURRENCY}) (?P<orig>{AMOUNT}))?"
    rf" TWD (?P<amt>{INTEGER})(?: [AGQS])?$",
    re.ASCII,
)
# A payment: no posting date.
_PAYMENT = re.compile(r"^(?P<date>\d\d/\d\d) (?P<desc>\D.*) TWD (?P<amt>-\d[\d,]*)$", re.ASCII)
# 點數明細, its headings an image (ADR 0019): "<上期累積點數> 點 <本期加點> 點 <本期調整點數> 點 <本期扣點> 點
# <本期結餘點數> 點". The table names no program; its notes call the points e point. From the October 2026
# bill it is 玉山e point明細, without 本期調整點數.
_POINTS = re.compile(
    rf"^(?P<opening>{INTEGER}) 點 (?P<added>{INTEGER}) 點(?: (?P<adjusted>{INTEGER}) 點)? (?P<used>{INTEGER}) 點"
    rf" (?P<closing>{INTEGER}) 點$"
)
_POINTS_NAME = "e point"
# e point lasts a year from the day it is added (玉山 e point FAQ: 點數效期1年). A bill adds what came in
# since the last one closed, a month before: a year on, the end of the 11th month after the closing's is
# the latest any of it lapses by, or a few days early for what came in on the closing month.
_POINTS_LAPSE_MONTHS = 11
# "※您有e point點數即將到期：2026/10共0點、2026/11共0點，…": each month's, by its last day.
_POINTS_EXPIRING = re.compile(r"(?P<y>\d{4})/(?P<m>\d{1,2})共(?P<n>[\d,]+)點")


class EsunParser(StatementParser):
    name = "esun.credit_card"
    version = "6"
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
        closing = either_date(find(_CLOSING, texts, "closing date").group(1))
        due = either_date(find(_DUE, texts, "due date").group(1))
        subtotal = amount(find(_SUBTOTAL, texts, "本期合計").group(1))
        total_due = amount(find(_TOTAL_DUE, texts, "本期應繳總金額").group(1))

        lines: list[Line] = []
        program: re.Match[str] | None = None
        expiries: list[Expiry] = []
        section = ""
        card = ""
        for line in (line for text in texts for line in text.splitlines()):
            if match := _POINTS.match(line):
                program = match
            elif "即將到期" in line and _POINTS_NAME in line:
                expiries = [
                    Expiry(_POINTS_NAME, amount(m["n"]), month_end(int(m["y"]), int(m["m"])))
                    for m in _POINTS_EXPIRING.finditer(line)
                ]
            elif _FEES.match(line):
                section, card = "fees", ""
            elif line.startswith("本期消費明細"):
                section = "purchases"
            elif line.startswith("本期合計"):
                section = ""
            elif match := _CARD.match(line):
                card = match.group(1)
            elif match := _PAYMENT.match(line):
                lines.append(
                    card_line(
                        month_day(match["date"], closing), clean(match["desc"]), LineKind.PAYMENT, amount(match["amt"])
                    )
                )
            elif section and (match := _ROW.match(line)):
                description = clean(match["desc"])
                value = amount(match["amt"])
                # Foreign merchants that bill in TWD print TWD as the original currency too.
                foreign = bool(match["cur"]) and match["cur"] != "TWD"
                lines.append(
                    card_line(
                        month_day(match["date"], closing),
                        description,
                        card_kind(description, value),
                        value,
                        posting_date=month_day(match["posted"], closing),
                        original_amount=amount(match["orig"]) if foreign else None,
                        original_currency=match["cur"] if foreign else "",
                        card_last4=card if section == "purchases" else "",
                    )
                )

        programs = []
        if program:
            programs.append(
                points_program(
                    _POINTS_NAME,
                    opening=amount(program["opening"]),
                    moves=(
                        earned("本期加點", amount(program["added"])),
                        adjusted("本期調整點數", amount(program["adjusted"] or "0")),
                        used("本期扣點", amount(program["used"])),
                    ),
                    closing=amount(program["closing"]),
                    expiries=expiries,
                    lapse=lapse_months_after_closing(_POINTS_NAME, _POINTS_LAPSE_MONTHS),
                )
            )

        statement = card_bill(
            self.institution,
            closing=closing,
            due=due,
            total_due=total_due,
            lines=lines,
            checks=[check("本期合計", new_charges(lines), subtotal)],
            programs=programs,
        )
        return Parsed(statement)
