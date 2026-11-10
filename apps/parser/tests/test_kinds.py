"""One set of classification rules for every bank's lines (ADR 0013); descriptions are synthetic (ADR 0006)."""

from decimal import Decimal

import pytest

from finchie_parser.model import DepositKind, Kind
from finchie_parser.parsers.kinds import card_kind, card_kind_by_words, deposit_kind, is_charge


@pytest.mark.parametrize(
    ("description", "amount", "kind"),
    [
        ("點數折抵－測試商店", "-30", Kind.REBATE),
        ("信用卡消費折抵_測試百貨", "-45", Kind.REBATE),
        ("一般消費1%現金回饋", "-12", Kind.REBATE),
        ("國外交易服務費", "5", Kind.FOREIGN_TRANSACTION_FEE),
        ("國外交易手續費 CAFE", "6", Kind.FOREIGN_TRANSACTION_FEE),
        ("國外退貨手續費", "-6", Kind.FOREIGN_TRANSACTION_FEE),
        ("商務卡年費", "1800", Kind.ANNUAL_FEE),
        ("商務卡年費減免", "-1800", Kind.ANNUAL_FEE),
        ("台新銀行帳戶自動轉帳扣繳", "-2000", Kind.PAYMENT),
        ("您的付款已收到,謝謝您!", "-2000", Kind.PAYMENT),
        ("測試商店", "-80", Kind.REFUND),
        ("測試商店", "80", Kind.PURCHASE),
    ],
)
def test_a_card_line_is_classified_by_its_words_then_its_sign(description: str, amount: str, kind: Kind) -> None:
    assert card_kind(description, Decimal(amount)) is kind


def test_points_redeemed_against_the_bill_are_a_rebate_not_a_refund() -> None:
    # parser.proto: REBATE is "cash back and points redeemed against the bill".
    assert card_kind("點數折抵", Decimal(-100)) is Kind.REBATE


def test_a_fee_given_back_stays_a_fee_even_worded_as_cash_back() -> None:
    assert card_kind("國外交易手續費回饋", Decimal(-6)) is Kind.FOREIGN_TRANSACTION_FEE


def test_payment_words_on_a_charge_do_not_make_it_a_payment() -> None:
    assert card_kind("繳款手續費", Decimal(15)) is Kind.PURCHASE


def test_a_line_without_any_of_the_words_has_no_kind_by_words() -> None:
    assert card_kind_by_words("循環利息", Decimal(30)) is None
    assert is_charge("循環利息")


@pytest.mark.parametrize("description", ["利息", "利息存入", "存款息", "INTEREST"])
def test_every_banks_interest_wording_is_interest_whichever_bank_prints_it(description: str) -> None:
    assert deposit_kind(description) is DepositKind.INTEREST


@pytest.mark.parametrize("description", ["ATM跨行轉", "利息稅", "定存利息轉入其他帳戶", ""])
def test_a_deposit_line_is_interest_only_when_its_whole_description_says_so(description: str) -> None:
    assert deposit_kind(description) is DepositKind.OTHER
