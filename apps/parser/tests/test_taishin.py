from datetime import date
from decimal import Decimal

import pytest
from fakes import FakeDocument, card

from finchie_parser.model import Kind, PointsKind, Statement
from finchie_parser.parsers.taishin import TaishinParser

PAGE_1 = """115年 03月 信用卡電子帳單
帳 務 資 訊
帳單結帳日 115/04/07
繳款截止日 115/04/22
上期應繳總額 2,000
-已繳退款總額 2,000
=前期餘額 0
+本期新增款項 3,170
=本期累計應繳金額 3,170
本期最低應繳金額 1,000"""

PAGE_2 = """消費日 入帳起息日消費明細 新臺幣金額 外幣折算日 消費地 幣別 外幣金額
台新銀行帳戶自動轉帳扣繳台新信用
115/03/24 115/03/24 -2,000
卡款
Richart卡(原測試御璽) (卡號末四碼:1111)
115/03/05 115/03/07 統一超商－測試店A9999 TAIPEI 65 TW
非常長的商店名稱股份有限公司A9999
115/03/06 115/03/09 1,500 TW
TAIPEI
115/03/10 115/03/11 BOOKSTORE ONLINEO1234 SEATTL 640 0309 US USD 20.00
115/03/10 115/03/11 國外交易服務費－640.00 10
Richart卡(原測試商務 (卡號末四碼:2222)
115/03/15 115/03/16 HOTEL BOOKINGO9157 Intern -800 SG
信用卡消費折抵_測試百貨股份有限
115/03/16 115/03/18 -45
公司
115/04/07 115/04/07 商務卡年費 1,800 TW
115/04/07 115/04/07 商務卡年費減免 -1,800 TW
115/03/20 115/03/22 健身中心 1,800 TW
自民國115年06月結帳日之次日起，您適用之循環信用利率為5.00%"""


def parse() -> Statement:
    return card(TaishinParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None))


def test_taishin_reads_statement_fields() -> None:
    statement = parse()
    assert statement.closing_date == date(2026, 4, 7)
    assert statement.due_date == date(2026, 4, 22)
    assert statement.total_due == Decimal(3170)
    assert statement.reconciliation.balanced


def test_taishin_joins_wrapped_descriptions() -> None:
    descriptions = [tx.description for tx in parse().transactions]
    assert "台新銀行帳戶自動轉帳扣繳台新信用 卡款" in descriptions
    assert "非常長的商店名稱股份有限公司A9999 TAIPEI" in descriptions
    assert "信用卡消費折抵_測試百貨股份有限 公司" in descriptions
    # The line after a wrapped row is consumed, not parsed again.
    assert len(descriptions) == 10


@pytest.mark.parametrize("wording", ["台新自動扣款", "您的付款已收到,謝謝您!"])
def test_taishin_reads_older_bills_wordings_of_a_payment(wording: str) -> None:
    older = PAGE_2.replace(
        "台新銀行帳戶自動轉帳扣繳台新信用\n115/03/24 115/03/24 -2,000\n卡款", f"115/03/24 115/03/24 {wording} -2,000"
    )
    statement = card(TaishinParser().parse(FakeDocument([PAGE_1, older]), ocr=None))
    assert {tx.description: tx.kind for tx in statement.transactions}[wording] is Kind.PAYMENT
    assert statement.reconciliation.balanced


def test_taishin_classifies_lines() -> None:
    kinds = {tx.description: tx.kind for tx in parse().transactions}
    assert kinds["台新銀行帳戶自動轉帳扣繳台新信用 卡款"] is Kind.PAYMENT
    assert kinds["國外交易服務費-640.00"] is Kind.FOREIGN_TRANSACTION_FEE
    assert kinds["HOTEL BOOKINGO9157 Intern"] is Kind.REFUND
    assert kinds["信用卡消費折抵_測試百貨股份有限 公司"] is Kind.REBATE
    assert kinds["商務卡年費"] is Kind.ANNUAL_FEE
    assert kinds["商務卡年費減免"] is Kind.ANNUAL_FEE


def test_taishin_tracks_cards_countries_and_foreign_amounts() -> None:
    by_description = {tx.description: tx for tx in parse().transactions}
    assert by_description["台新銀行帳戶自動轉帳扣繳台新信用 卡款"].card_last4 == ""
    assert by_description["統一超商-測試店A9999 TAIPEI"].card_last4 == "1111"
    assert by_description["健身中心"].card_last4 == "2222"
    book = by_description["BOOKSTORE ONLINEO1234 SEATTL"]
    assert (book.country, book.original_currency, book.original_amount) == ("US", "USD", Decimal("20.00"))
    assert by_description["HOTEL BOOKINGO9157 Intern"].country == "SG"


FOREIGN_REFUND = """消費日 入帳起息日消費明細 新臺幣金額 外幣折算日 消費地 幣別 外幣金額
Richart卡(原測試御璽) (卡號末四碼:1111)
115/03/12 115/03/14 ONLINE MARKET SHANGH 280 0313 CN CNY 60.50
115/03/31 115/04/02 ONLINE MARKET SHANGH -125 0401 CN CNY -27.03"""


def test_taishin_reads_refunds_of_foreign_purchases() -> None:
    page_1 = PAGE_1.replace("3,170", "155")
    statement = card(TaishinParser().parse(FakeDocument([page_1, FOREIGN_REFUND]), ocr=None))
    refund = statement.transactions[-1]
    # Not 3 read out of "-27.03": the foreign amount is negative too.
    assert (refund.kind, refund.amount) == (Kind.REFUND, Decimal(-125))
    assert (refund.original_currency, refund.original_amount, refund.country) == ("CNY", Decimal("-27.03"), "CN")
    assert statement.reconciliation.balanced


# The last row of page 2 has its description on the line above; page 3 opens with the statement's
# header, which names the due date.
PAGE_BREAK_2 = """消費日 入帳起息日消費明細 新臺幣金額 外幣折算日 消費地 幣別 外幣金額
Richart卡(原測試御璽) (卡號末四碼:1111)
115/03/05 115/03/07 統一超商－測試店A9999 TAIPEI 65 TW
網路商店退款
115/03/06 115/03/09 -40 TW"""

PAGE_BREAK_3 = """繳款截止日 115/04/22
消費日 入帳起息日消費明細 新臺幣金額 外幣折算日 消費地 幣別 外幣金額
115/03/20 115/03/22 健身中心 1,800 TW"""


def test_taishin_never_joins_a_description_across_a_page_break() -> None:
    page_1 = PAGE_1.replace("3,170", "1,825")
    statement = card(TaishinParser().parse(FakeDocument([page_1, PAGE_BREAK_2, PAGE_BREAK_3]), ocr=None))
    rows = [(tx.description, tx.kind, tx.amount, tx.card_last4) for tx in statement.transactions]
    assert rows == [
        ("統一超商-測試店A9999 TAIPEI", Kind.PURCHASE, Decimal(65), "1111"),
        ("網路商店退款", Kind.REFUND, Decimal(-40), "1111"),
        ("健身中心", Kind.PURCHASE, Decimal(1800), "1111"),
    ]
    assert statement.reconciliation.balanced


POINTS = """台新Point
亞洲萬里通里數
(信用卡)點數
上期結餘點數/里數 2,175 **
新增回饋 129 **
活動回饋/調整 0 **
本期使用點數/里數 -2,175 **
本期結餘回饋 129 **
■台新Point(信用卡)點數到期提醒：您有129點將於
117/01/31到期，請至台新銀行網站查詢/兌換。"""


def test_taishin_reads_its_points_with_used_points_printed_negative() -> None:
    parsed = TaishinParser().parse(FakeDocument([PAGE_1 + "\n" + POINTS, PAGE_2]), ocr=None)
    [program] = card(parsed).points
    assert (program.name, program.closing) == ("台新Point(信用卡)", Decimal(129))
    assert [(line.label, line.kind, line.amount) for line in program.lines] == [
        ("新增回饋", PointsKind.EARN, Decimal(129)),
        ("本期使用點數/里數", PointsKind.REDEEM, Decimal(-2175)),
    ]
    assert [(e.amount, e.by) for e in program.expiries] == [(Decimal(129), date(2028, 1, 31))]
    assert parsed.warnings == []


def test_taishin_takes_the_expiry_only_from_its_reminder() -> None:
    promotion = "活動期間您有500點將於117/12/31到期前加碼回饋。\n"
    parsed = TaishinParser().parse(FakeDocument([PAGE_1 + "\n" + promotion + POINTS, PAGE_2]), ocr=None)
    [program] = card(parsed).points
    assert [(e.amount, e.by) for e in program.expiries] == [(Decimal(129), date(2028, 1, 31))]


def test_taishin_reads_each_column_with_figures_as_a_program_under_its_name_of_the_time() -> None:
    # Before 2022-07 the points were 大集大利點數; a card that earns miles fills the second column.
    old = """准及調整額度之權利。
大集大利點數 亞洲萬里通里數
上期結餘點數/里數 0 1,000
新增回饋 0 200
活動回饋/調整 0 0
本期使用點數/里數 0 -300
本期結餘回饋 0 900
■【大集大利點數到期提醒】 您有0點將於110/01/31到期,請至下述網址查詢/兌換。"""
    parsed = TaishinParser().parse(FakeDocument([PAGE_1 + "\n" + old, PAGE_2]), ocr=None)
    programs = [(p.name, p.closing, [(e.amount, e.by) for e in p.expiries]) for p in card(parsed).points]
    assert programs == [
        ("大集大利點數", Decimal(0), [(Decimal(0), date(2021, 1, 31))]),
        ("亞洲萬里通里數", Decimal(900), []),
    ]
    assert parsed.warnings == []


def test_taishin_bill_without_a_points_table_has_no_points() -> None:
    assert parse().points == []


def test_taishin_takes_cash_back_as_a_rebate() -> None:
    page = PAGE_2.replace("115/03/16 115/03/18 -45", "115/03/16 115/03/18 Richart現金回饋 -45")
    statement = card(TaishinParser().parse(FakeDocument([PAGE_1, page]), ocr=None))
    assert [(tx.description, tx.kind) for tx in statement.transactions if tx.amount == -45] == [
        ("Richart現金回饋", Kind.REBATE)
    ]
