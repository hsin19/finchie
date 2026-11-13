from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, account, card, card_detail, card_lines, check

from finchie_parser.errors import LayoutError
from finchie_parser.model import AccountKind, Balance, CardBill, Check, Expiry, LapseRule, LineKind, Statement
from finchie_parser.parsers.cathay import CathayParser

PAGE_1 = """信用卡帳單 115年04月
王Ｏ明 先生
115/04/01 80,000"""

PAGE_2 = """您本月消費明細如下：
上期帳單總額 500
03/18 03/18 本行自動扣繳 -500
繳款小計 -500
03/05 03/08 測試計程車隊 250 4321 TW TWD
03/09 03/10 台灣麥當勞ＭＯＰ－１ 180 4321 8765 TW TWD
03/12 03/14 CAFE PARIS 600 4321 FR EUR 17.50 03/12
正卡本期消費 1,030
本期應繳總額 1,030
◎ 您的新臺幣帳款將於 115/04/17 (遇假日順延)之次一營業日自帳號扣款1,030元整
國泰世華CUBE App"""


def parse() -> Statement:
    return card(CathayParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None))


def test_cathay_reads_statement_fields() -> None:
    statement = parse()
    assert statement.closing_date == date(2026, 4, 1)
    assert statement.card_bill == CardBill(date(2026, 4, 17), "TWD", Decimal(1030))
    assert statement.checks == [
        Check("本期消費", "TWD", Decimal(1030), Decimal(1030)),
        Check("繳款小計", "TWD", Decimal(-500), Decimal(-500)),
    ]


def test_cathay_parses_rows_with_and_without_wallet_card() -> None:
    rows = [(line.description, line.kind, line.amount, card_detail(line).card_last4) for line in card_lines(parse())]
    assert rows == [
        ("本行自動扣繳", LineKind.PAYMENT, Decimal(-500), ""),
        ("測試計程車隊", LineKind.PURCHASE, Decimal(250), "4321"),
        ("台灣麥當勞MOP-1", LineKind.PURCHASE, Decimal(180), "4321"),
        ("CAFE PARIS", LineKind.PURCHASE, Decimal(600), "4321"),
    ]


def test_cathay_records_foreign_amounts_only_for_foreign_currency() -> None:
    by_description = {line.description: card_detail(line) for line in card_lines(parse())}
    cafe = by_description["CAFE PARIS"]
    assert (cafe.country, cafe.original_currency, cafe.original_amount) == ("FR", "EUR", Decimal("17.50"))
    taxi = by_description["測試計程車隊"]
    assert (taxi.country, taxi.original_currency, taxi.original_amount) == ("TW", "", None)


def test_cathay_is_recognized_by_text_on_page_2() -> None:
    assert CathayParser().matches_text(FakeDocument([PAGE_1, PAGE_2]))


def test_cathay_without_a_printed_subtotal_is_a_layout_error() -> None:
    # Unrecognized rows and no subtotal would otherwise reconcile as 0 == 0 with no transactions.
    unreadable = "\n".join(line for line in PAGE_2.splitlines() if not line.startswith(("03/", "正卡")))
    with pytest.raises(LayoutError, match="本期消費"):
        CathayParser().parse(FakeDocument([PAGE_1, unreadable]), ocr=None)


FEES_PAGE = """您本月消費明細如下：
03/12 03/14 CAFE PARIS 600 4321 FR EUR 17.50 03/12
03/12 04/01 國外交易手續費 -CAFEPA 9 4321
04/01 04/01 CUBE 卡年費優惠 0 4321
正卡本期消費 609
本期應繳總額 609
◎ 您的新臺幣帳款將於 115/04/17 (遇假日順延)之次一營業日自帳號扣款609元整
國泰世華CUBE App"""


def test_cathay_reads_fees_without_country_or_currency() -> None:
    statement = card(CathayParser().parse(FakeDocument([PAGE_1, FEES_PAGE]), ocr=None))
    rows = [(line.description, line.kind, line.amount, card_detail(line).card_last4) for line in card_lines(statement)]
    assert rows[1:] == [
        ("國外交易手續費 -CAFEPA", LineKind.FOREIGN_TRANSACTION_FEE, Decimal(9), "4321"),
        ("CUBE 卡年費優惠", LineKind.ANNUAL_FEE, Decimal(0), "4321"),
    ]
    # No 繳款小計 printed: nothing to check payments against.
    assert [c.name for c in statement.checks] == ["本期消費"]
    assert statement.balanced


# A tax-free refund prints a negative foreign amount, and the fee charged on the purchase comes back
# as a negative "國外退貨手續費" line.
REFUND_PAGE = """您本月消費明細如下：
03/12 03/14 CAFE PARIS 600 4321 FR EUR 17.50 03/12
03/12 04/01 國外交易手續費 -CAFEPA 9 4321
03/20 04/01 國外退貨手續費 -TAXFRE -1 4321
03/20 03/24 TAX FREE REFUND SHOP -95 4321 FR EUR -2.80 03/22
正卡本期消費 513
本期應繳總額 513
◎ 您的新臺幣帳款將於 115/04/17 (遇假日順延)之次一營業日自帳號扣款513元整
國泰世華CUBE App"""


def test_cathay_reads_foreign_refunds_and_fee_refunds() -> None:
    statement = card(CathayParser().parse(FakeDocument([PAGE_1, REFUND_PAGE]), ocr=None))
    rows = [
        (
            line.description,
            line.kind,
            line.amount,
            card_detail(line).original_amount,
            card_detail(line).original_currency,
        )
        for line in card_lines(statement)
    ]
    assert rows[2:] == [
        ("國外退貨手續費 -TAXFRE", LineKind.FOREIGN_TRANSACTION_FEE, Decimal(-1), None, ""),
        ("TAX FREE REFUND SHOP", LineKind.REFUND, Decimal(-95), Decimal("-2.80"), "EUR"),
    ]
    assert check(statement, "本期消費").balanced


PAYMENTS_PAGE = """您本月消費明細如下：
上期帳單總額 1,500
03/18 03/18 本行自動扣繳 -500
03/20 03/20 ATM轉帳 822 -1,000
繳款小計 -1,500
03/05 03/08 測試計程車隊 250 4321 TW TWD
正卡本期消費 250
本期應繳總額 250
◎ 您的新臺幣帳款將於 115/04/17 (遇假日順延)之次一營業日自帳號扣款250元整
國泰世華CUBE App"""


def test_cathay_reads_payments_whose_description_has_digits() -> None:
    parsed = CathayParser().parse(FakeDocument([PAGE_1, PAYMENTS_PAGE]), ocr=None)
    lines = card_lines(card(parsed))
    payments = [(line.description, line.amount) for line in lines if line.kind is LineKind.PAYMENT]
    assert payments == [("本行自動扣繳", Decimal(-500)), ("ATM轉帳 822", Decimal(-1000))]
    assert check(card(parsed), "繳款小計").balanced
    assert parsed.warnings == []


def test_cathay_checks_payments_against_the_printed_subtotal_on_their_own() -> None:
    page = PAYMENTS_PAGE.replace("繳款小計 -1,500", "繳款小計 -2,000")
    parsed = CathayParser().parse(FakeDocument([PAGE_1, page]), ocr=None)
    statement = card(parsed)
    assert check(statement, "繳款小計") == Check("繳款小計", "TWD", Decimal(-1500), Decimal(-2000))
    assert not check(statement, "繳款小計").balanced
    assert not statement.balanced
    # The new charges still balance: payments are not part of them.
    assert check(statement, "本期消費").balanced
    assert parsed.warnings == []


# 信用卡回饋總覽 on page 1; its headings are unmapped glyphs, so only the row and the note are text.
POINTS = """小樹點(信用卡) 3,177 29 0 0 3,206
◎提醒您，您有88點小樹點(信用卡)於本月到期；共計有669點小樹點(信用卡)將於116年3月31日前陸續到期。"""


def test_cathay_reads_its_points_and_what_lapses_by_when() -> None:
    parsed = CathayParser().parse(FakeDocument([PAGE_1 + "\n" + POINTS, PAGE_2]), ocr=None)
    statement = card(parsed)
    points = account(statement, AccountKind.POINTS)
    assert points.balances == [Balance("小樹點(信用卡)", date(2026, 4, 1), Decimal(3206))]
    # The closing month's by its last day; the next twelve months' together, that month's included.
    assert points.expiries == [
        Expiry("小樹點(信用卡)", Decimal(88), date(2026, 4, 30)),
        Expiry("小樹點(信用卡)", Decimal(669), date(2027, 3, 31)),
    ]
    # Three years by month from the month after they are added: the month before a bill closing on the 1st.
    assert points.lapse_rules == [LapseRule("小樹點(信用卡)", months_after_closing=35)]
    assert check(statement, "小樹點(信用卡)").balanced
    assert parsed.warnings == []


def test_cathay_takes_redeemed_points_off_and_reports_points_that_do_not_add_up_as_a_check() -> None:
    parsed = CathayParser().parse(FakeDocument([PAGE_1 + "\n小樹點(信用卡) 3,161 107 0 100 3,170", PAGE_2]), ocr=None)
    statement = card(parsed)
    points = account(statement, AccountKind.POINTS)
    assert points.balances == [Balance("小樹點(信用卡)", date(2026, 4, 1), Decimal(3170))]
    # Only columns with a figure are lines, under the bill's labels, on the closing date; points used are negative.
    assert [(line.date, line.description, line.kind, line.amount) for line in points.lines] == [
        (date(2026, 4, 1), "本期新增/扣除", LineKind.EARN, Decimal(107)),
        (date(2026, 4, 1), "本期兌/轉換", LineKind.REDEEM, Decimal(-100)),
    ]
    assert check(statement, "小樹點(信用卡)") == Check("小樹點(信用卡)", "小樹點(信用卡)", Decimal(3168), Decimal(3170))
    assert not statement.balanced
    assert parsed.warnings == []


def test_cathay_bill_without_a_points_table_has_no_points() -> None:
    statement = parse()
    assert [a.kind for a in statement.accounts] == [AccountKind.CREDIT_CARD]
    assert [c.name for c in statement.checks] == ["本期消費", "繳款小計"]


def test_cathay_takes_points_redeemed_against_the_bill_as_a_rebate() -> None:
    page = PAGE_2.replace("正卡本期消費 1,030", "03/20 03/21 小樹點折抵 -30 4321 TW TWD\n正卡本期消費 1,000")
    statement = card(CathayParser().parse(FakeDocument([PAGE_1, page]), ocr=None))
    assert [(line.description, line.kind) for line in card_lines(statement) if line.amount == -30] == [
        ("小樹點折抵", LineKind.REBATE)
    ]
    assert check(statement, "本期消費").balanced
