from datetime import date
from decimal import Decimal

from fakes import FakeDocument, account, card, card_detail, card_lines, check

from finchie_parser.model import AccountKind, Balance, CardBill, Expiry, LapseRule, LineKind, Statement
from finchie_parser.parsers.esun import EsunParser

PAGE_1 = """親愛的 王＊明 您好：
這是您 115年03月 信用卡帳單，感謝您使用電子帳單讓地球更美麗，
1,226 元 120,000 元
115/04/22 11.88%
1,000 元 至 115/04
115/04/07 12,000 / 120,000 元"""

PAGE_2 = """繳款幣別 上期未繳餘額 本期新增款項 本期應繳總金額 本期最低應繳金額
TWD 0 1,226 1,226 1,000
上期應繳金額： TWD 500
03/24 感謝您辦理本行自動轉帳繳款！ TWD -500
本期費用明細：
04/07 04/07 ＵＢｅａｒ卡一般消費１％現金回饋 TWD -12
本期消費明細：
卡號：4000-XXXX-XXXX-1234（Unicard－正卡）
03/08 03/10 高鐵台北站 TWD 700 A
卡號：5500-XXXX-XXXX-5678（U Bear卡－正卡）
03/12 03/15 連支＊巷口麵店 TWD 120
03/18 03/19 STREAMING*SERVICE USA SAN JOSE 03/19 TWD 300 TWD 300
03/18 03/19 國外交易服務費 TWD 5
03/20 03/21 BOOKSHOP TOKYO 03/20 JPY 2,000 TWD 113
本期合計： TWD 1,226
本期應繳總金額： TWD 1,226"""


def parse() -> Statement:
    return card(EsunParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None))


def test_esun_reads_statement_fields() -> None:
    statement = parse()
    assert statement.closing_date == date(2026, 4, 7)
    assert statement.card_bill == CardBill(date(2026, 4, 22), "TWD", Decimal(1226))
    subtotal = check(statement, "本期合計")
    assert subtotal.balanced
    assert (subtotal.unit, subtotal.stated) == ("TWD", Decimal(1226))


def test_esun_card_account_has_no_balance_and_the_bill_carries_what_it_owes() -> None:
    statement = parse()
    card_account = account(statement, AccountKind.CREDIT_CARD)
    # The caller takes the card account's balance from the bill's total due (ADR 0022).
    assert (card_account.number, card_account.balances, card_account.expiries) == ("", [], [])
    assert {line.unit for line in card_account.lines} == {"TWD"}
    assert statement.card_bill == CardBill(due_date=date(2026, 4, 22), currency="TWD", total_due=Decimal(1226))


def test_esun_bill_without_points_reports_only_its_card_account_and_its_own_check() -> None:
    statement = parse()
    assert [a.kind for a in statement.accounts] == [AccountKind.CREDIT_CARD]
    assert [c.name for c in statement.checks] == ["本期合計"]


def test_esun_classifies_lines() -> None:
    kinds = [(line.description, line.kind, line.amount) for line in card_lines(parse())]
    assert kinds == [
        ("感謝您辦理本行自動轉帳繳款!", LineKind.PAYMENT, Decimal(-500)),
        ("UBear卡一般消費1%現金回饋", LineKind.REBATE, Decimal(-12)),
        ("高鐵台北站", LineKind.PURCHASE, Decimal(700)),
        ("連支*巷口麵店", LineKind.PURCHASE, Decimal(120)),
        ("STREAMING*SERVICE USA SAN JOSE", LineKind.PURCHASE, Decimal(300)),
        ("國外交易服務費", LineKind.FOREIGN_TRANSACTION_FEE, Decimal(5)),
        ("BOOKSHOP TOKYO", LineKind.PURCHASE, Decimal(113)),
    ]


def test_esun_tracks_cards_and_foreign_amounts() -> None:
    by_description = {line.description: card_detail(line) for line in card_lines(parse())}
    assert by_description["高鐵台北站"].card_last4 == "1234"
    assert by_description["連支*巷口麵店"].card_last4 == "5678"
    assert by_description["UBear卡一般消費1%現金回饋"].card_last4 == ""
    book = by_description["BOOKSHOP TOKYO"]
    assert (book.original_currency, book.original_amount) == ("JPY", Decimal(2000))
    # Billed in TWD by a foreign merchant: no foreign amount to record.
    streaming = by_description["STREAMING*SERVICE USA SAN JOSE"]
    assert (streaming.original_currency, streaming.original_amount) == ("", None)


def test_esun_flags_a_statement_that_does_not_add_up() -> None:
    tampered = PAGE_2.replace("本期合計： TWD 1,226", "本期合計： TWD 1,300")
    statement = card(EsunParser().parse(FakeDocument([PAGE_1, tampered]), ocr=None))
    subtotal = check(statement, "本期合計")
    assert not subtotal.balanced
    assert subtotal.computed == Decimal(1226)
    assert not statement.balanced


# 點數明細 on page 1; its headings are an image, so only the figures are text.
POINTS = """28 點 122 點 0 點 0 點 150 點
※e point統計區間為2026/3/7~2026/4/7。
※您有e point點數即將到期：2026/4共0點、2026/5共35點，將依效期每日到期清除，實際到期點數依系統為準。"""


def test_esun_reads_its_e_points_and_each_months_expiring_ones() -> None:
    parsed = EsunParser().parse(FakeDocument([PAGE_1 + "\n" + POINTS, PAGE_2]), ocr=None)
    statement = card(parsed)
    points = account(statement, AccountKind.POINTS)
    assert (points.number, points.balances) == ("", [Balance("e point", date(2026, 4, 7), Decimal(150))])
    lines = [(line.date, line.unit, line.description, line.kind, line.amount) for line in points.lines]
    assert lines == [(date(2026, 4, 7), "e point", "本期加點", LineKind.EARN, Decimal(122))]
    assert points.expiries == [
        Expiry("e point", Decimal(0), date(2026, 4, 30)),
        Expiry("e point", Decimal(35), date(2026, 5, 31)),
    ]
    # A year from the day added, in the month before the bill's or its own: by the 11th month's end at most.
    assert points.lapse_rules == [LapseRule("e point", months_after_closing=11)]
    program = check(statement, "e point")
    assert (program.unit, program.computed, program.stated, program.balanced) == (
        "e point",
        Decimal(150),
        Decimal(150),
        True,
    )
    assert parsed.warnings == []


def test_esun_checks_its_points_against_their_closing_without_a_warning() -> None:
    tampered = POINTS.replace("0 點 150 點", "0 點 151 點")
    parsed = EsunParser().parse(FakeDocument([PAGE_1 + "\n" + tampered, PAGE_2]), ocr=None)
    program = check(card(parsed), "e point")
    assert (program.computed, program.stated, program.balanced) == (Decimal(150), Decimal(151), False)
    assert check(card(parsed), "本期合計").balanced
    assert parsed.warnings == []


# From the October 2026 bill: Gregorian dates, rebates under 本期回饋／費用明細, and no 本期調整點數 column.
PAGE_1_GREGORIAN = """親愛的 王＊明 您好：
這是您 2026年10月 信用卡帳單，感謝您使用電子帳單讓地球更美麗，
1,226 元 120,000 元
2026/11/23 11.88%
1,000 元 至 2026/12
2026/11/07 12,000 / 120,000 元
150 點 35 點 20 點 165 點
※玉山 e point 統計區間為20261007~20261106。
※您有玉山 e point 即將到期：2026/11共0點、2026/12共10點，將依效期每日到期清除，實際到期點數依系統為準。"""

PAGE_2_GREGORIAN = (
    PAGE_2.replace("03/24 ", "10/22 ")
    .replace("本期費用明細：", "本期回饋／費用明細：")
    .replace("04/07 04/07", "11/07 11/07")
    .replace("03/", "10/")
)


def test_esun_reads_the_gregorian_layout_from_october_2026() -> None:
    parsed = EsunParser().parse(FakeDocument([PAGE_1_GREGORIAN, PAGE_2_GREGORIAN]), ocr=None)
    statement = card(parsed)
    assert statement.closing_date == date(2026, 11, 7)
    assert statement.card_bill is not None
    assert statement.card_bill.due_date == date(2026, 11, 23)
    assert statement.balanced
    rebate = next(line for line in card_lines(statement) if line.kind is LineKind.REBATE)
    assert (rebate.date, rebate.amount) == (date(2026, 11, 7), Decimal(-12))
    points = account(statement, AccountKind.POINTS)
    assert points.balances == [Balance("e point", date(2026, 11, 7), Decimal(165))]
    lines = [(line.description, line.kind, line.amount) for line in points.lines]
    assert lines == [("本期加點", LineKind.EARN, Decimal(35)), ("本期扣點", LineKind.REDEEM, Decimal(-20))]
    assert [(e.amount, e.by) for e in points.expiries] == [
        (Decimal(0), date(2026, 11, 30)),
        (Decimal(10), date(2026, 12, 31)),
    ]
    assert check(statement, "e point").balanced
    assert parsed.warnings == []


def test_esun_takes_points_redeemed_against_the_bill_as_a_rebate() -> None:
    page = PAGE_2.replace("04/07 04/07 ＵＢｅａｒ卡一般消費１％現金回饋 TWD -12", "04/07 04/07 e point點數折抵 TWD -12")
    assert page != PAGE_2
    statement = card(EsunParser().parse(FakeDocument([PAGE_1, page]), ocr=None))
    assert [(line.description, line.kind) for line in card_lines(statement) if line.amount == -12] == [
        ("e point點數折抵", LineKind.REBATE)
    ]
