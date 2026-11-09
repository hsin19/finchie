from datetime import date
from decimal import Decimal

from fakes import FakeDocument

from finchie_parser.model import Kind, PointsKind
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


def parse():
    return EsunParser().parse(FakeDocument([PAGE_1, PAGE_2]), ocr=None).statement


def test_esun_reads_statement_fields() -> None:
    statement = parse()
    assert statement.closing_date == date(2026, 4, 7)
    assert statement.due_date == date(2026, 4, 22)
    assert statement.total_due == Decimal(1226)
    assert statement.reconciliation.balanced
    assert statement.reconciliation.stated == Decimal(1226)


def test_esun_classifies_lines() -> None:
    kinds = [(tx.description, tx.kind, tx.amount) for tx in parse().transactions]
    assert kinds == [
        ("感謝您辦理本行自動轉帳繳款!", Kind.PAYMENT, Decimal(-500)),
        ("UBear卡一般消費1%現金回饋", Kind.REBATE, Decimal(-12)),
        ("高鐵台北站", Kind.PURCHASE, Decimal(700)),
        ("連支*巷口麵店", Kind.PURCHASE, Decimal(120)),
        ("STREAMING*SERVICE USA SAN JOSE", Kind.PURCHASE, Decimal(300)),
        ("國外交易服務費", Kind.FOREIGN_TRANSACTION_FEE, Decimal(5)),
        ("BOOKSHOP TOKYO", Kind.PURCHASE, Decimal(113)),
    ]


def test_esun_tracks_cards_and_foreign_amounts() -> None:
    by_description = {tx.description: tx for tx in parse().transactions}
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
    statement = EsunParser().parse(FakeDocument([PAGE_1, tampered]), ocr=None).statement
    assert not statement.reconciliation.balanced
    assert statement.reconciliation.computed == Decimal(1226)


# 點數明細 on page 1; its headings are an image, so only the figures are text.
POINTS = """28 點 122 點 0 點 0 點 150 點
※e point統計區間為2026/3/7~2026/4/7。
※您有e point點數即將到期：2026/4共0點、2026/5共35點，將依效期每日到期清除，實際到期點數依系統為準。"""


def test_esun_reads_its_e_points_and_each_months_expiring_ones() -> None:
    parsed = EsunParser().parse(FakeDocument([PAGE_1 + "\n" + POINTS, PAGE_2]), ocr=None)
    [program] = parsed.statement.points
    assert (program.name, program.closing) == ("e point", Decimal(150))
    lines = [(line.label, line.kind, line.amount) for line in program.lines]
    assert lines == [("本期加點", PointsKind.EARN, Decimal(122))]
    assert [(e.amount, e.by) for e in program.expiries] == [
        (Decimal(0), date(2026, 4, 30)),
        (Decimal(35), date(2026, 5, 31)),
    ]
    assert parsed.warnings == []
